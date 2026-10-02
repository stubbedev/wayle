package openconnect

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/wayle/internal/fileuri"
)

// AnyConnect single sign-on through the system browser (sso.rs): the
// SAML path that does not need an embedded webview. The shapes come
// from openconnect's auth.c, cstp.c and hpke.c.
//
//  1. the client advertises single-sign-on-external-browser in its
//     <capabilities> and sends its ephemeral P-256 public key in
//     X-AnyConnect-STRAP-DH-Pubkey;
//  2. the gateway answers with an auth form containing an
//     <input type="sso"> and the sso-v2-login URL to open;
//  3. the client opens that URL in the browser and listens on
//     [::1]:29786 for the IdP to come back with
//     GET /api/sso/<base64 blob>?return=<url>, answering 302 to return
//     so the browser lands on the gateway's own success page;
//  4. the blob is a TLV structure carrying the gateway's ephemeral
//     public key, an AES-GCM ciphertext, its tag and IV. ECDH against
//     our key, HKDF-SHA256 with the info string AC_ECIES, then
//     AES-256-GCM open yields the SSO token;
//  5. the token goes back as the value of the sso input, and the
//     sign-in finishes like any other form.
//
// Opt-in per profile (wayle-sso): openconnect ships --no-external-auth
// because a gateway that sees the capability can insist on a browser
// where it would otherwise have served a form.

// ssoCallbackAddr is the loopback address Cisco's browser flow comes
// back on. Not configurable: the IdP redirects to it by absolute URL. A
// variable only so tests can listen elsewhere.
var ssoCallbackAddr = net.JoinHostPort("::1", strconv.Itoa(29786))

const (
	// ssoCallbackPath is the path the browser calls back on.
	ssoCallbackPath = "/api/sso/"
	// hkdfInfo is the HKDF info string from hpke.c: exactly eight bytes.
	hkdfInfo = "AC_ECIES"

	// TLV tags inside the token blob.
	tagPubkey     = 1
	tagAEADTag    = 2
	tagCiphertext = 3
	tagIV         = 4

	// AES-GCM's IV and tag are both 12 bytes here — the tag length is
	// Cisco's choice and hpke.c rejects anything else, so this does too.
	ssoIVLen  = 12
	ssoTagLen = 12

	// p256PointLen is an uncompressed P-256 point: 0x04 and two 32-byte
	// coordinates.
	p256PointLen = 65
)

// p256SPKIPrefix is the fixed DER prefix of a P-256
// SubjectPublicKeyInfo: SEQUENCE { SEQUENCE { OID ecPublicKey, OID
// prime256v1 }, BIT STRING (0 unused bits) }. The whole structure for
// this curve is this prefix followed by the 65-byte point, so wrapping
// and unwrapping is a splice rather than a parse.
var p256SPKIPrefix = []byte{
	0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a,
	0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
}

// ssoKeys is our ephemeral key for one sign-in.
type ssoKeys struct {
	private *ecdh.PrivateKey
	// publicBase64 is the public key as the gateway wants it: base64 of
	// the DER SubjectPublicKeyInfo.
	publicBase64 string
}

// generateSSOKeys makes a fresh key pair. One per sign-in: the shared
// secret it derives protects exactly one token.
func generateSSOKeys() (*ssoKeys, error) {
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, authError("cannot generate a key for the browser sign-in")
	}
	return &ssoKeys{
		private:      private,
		publicBase64: base64.StdEncoding.EncodeToString(spki(private.PublicKey().Bytes())),
	}, nil
}

// spki wraps an uncompressed P-256 point in a DER SubjectPublicKeyInfo.
func spki(point []byte) []byte {
	return append(append(make([]byte, 0, len(p256SPKIPrefix)+len(point)), p256SPKIPrefix...), point...)
}

// pointFromSPKI takes the uncompressed point back out of a DER
// SubjectPublicKeyInfo, refusing anything that is not a P-256 public key
// in that exact shape — a different curve derives a different secret,
// so guessing would produce a token that fails to decrypt with no
// explanation.
func pointFromSPKI(der []byte) ([]byte, bool) {
	rest, ok := bytes.CutPrefix(der, p256SPKIPrefix)
	if !ok || len(rest) != p256PointLen || rest[0] != 0x04 {
		return nil, false
	}
	return rest, true
}

// tokenBlob is the parts of the encrypted SSO token.
type tokenBlob struct {
	// pubkey is the gateway's ephemeral public key, DER SPKI.
	pubkey     []byte
	tag        []byte
	ciphertext []byte
	iv         []byte
}

// parseBlob decodes the TLV blob the browser handed back: a leading
// 0x0001, then (tag u16, len u16, bytes) triples, every field required,
// tag and IV exactly twelve bytes. A malformed blob says so rather than
// surfacing as an indistinguishable decryption failure later.
func parseBlob(raw []byte) (tokenBlob, error) {
	if len(raw) < 2 || binary.BigEndian.Uint16(raw) != 1 {
		return tokenBlob{}, authError("the sign-in token is not in the expected format")
	}
	fields := map[uint16][]byte{}
	for at := 2; at < len(raw); {
		if at+4 > len(raw) {
			return tokenBlob{}, authError("the sign-in token is truncated")
		}
		field := binary.BigEndian.Uint16(raw[at:])
		length := int(binary.BigEndian.Uint16(raw[at+2:]))
		start, end := at+4, at+4+length
		if end > len(raw) {
			return tokenBlob{}, authError("the sign-in token is truncated")
		}
		switch field {
		case tagPubkey, tagAEADTag, tagCiphertext, tagIV:
		default:
			return tokenBlob{}, authError("the sign-in token has an unexpected field")
		}
		if _, seen := fields[field]; seen {
			return tokenBlob{}, authError("the sign-in token repeats a field")
		}
		fields[field] = bytes.Clone(raw[start:end])
		at = end
	}

	for _, required := range []struct {
		tag    uint16
		reason string
	}{
		{tagPubkey, "the sign-in token has no gateway key"},
		{tagAEADTag, "the sign-in token has no authentication tag"},
		{tagCiphertext, "the sign-in token is empty"},
		{tagIV, "the sign-in token has no nonce"},
	} {
		if _, ok := fields[required.tag]; !ok {
			return tokenBlob{}, authError(required.reason)
		}
	}
	blob := tokenBlob{
		pubkey:     fields[tagPubkey],
		tag:        fields[tagAEADTag],
		ciphertext: fields[tagCiphertext],
		iv:         fields[tagIV],
	}
	if len(blob.tag) != ssoTagLen || len(blob.iv) != ssoIVLen {
		return tokenBlob{}, authError("the sign-in token's nonce or tag is the wrong size")
	}
	return blob, nil
}

// deriveSSOKey is ECDH against our key, then HKDF-SHA256 with an empty
// salt (what openconnect's EVP_PKEY_HKDF with no salt set amounts to).
func deriveSSOKey(keys *ssoKeys, gatewayPubkey []byte) ([]byte, error) {
	point, ok := pointFromSPKI(gatewayPubkey)
	if !ok {
		return nil, authError("the gateway sent a key on an unexpected curve")
	}
	peer, err := ecdh.P256().NewPublicKey(point)
	if err != nil {
		return nil, authError("cannot agree a key with the gateway")
	}
	secret, err := keys.private.ECDH(peer)
	if err != nil {
		return nil, authError("cannot agree a key with the gateway")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, hkdfInfo, 32)
	if err != nil {
		return nil, authError("cannot derive the sign-in token's key")
	}
	return key, nil
}

// newGCM12 is AES-256-GCM with a twelve-byte tag, not the usual
// sixteen: Cisco's choice, pinned by openconnect's openssl.c
// (EVP_CTRL_AEAD_SET_TAG, 12).
func newGCM12(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithTagSize(block, ssoTagLen)
}

// decryptToken opens the token. The plaintext has to be the
// alphanumeric token a gateway sends — openconnect's check, and what
// stops a wrong key from being reported as a working sign-in.
func decryptToken(keys *ssoKeys, blob tokenBlob) (string, error) {
	key, err := deriveSSOKey(keys, blob.pubkey)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM12(key)
	if err != nil {
		return "", authError("cannot use the derived key")
	}
	sealed := append(bytes.Clone(blob.ciphertext), blob.tag...)
	plain, err := gcm.Open(nil, blob.iv, sealed, nil)
	if err != nil {
		return "", authError("the sign-in token did not verify")
	}
	if !utf8.Valid(plain) {
		return "", authError("the sign-in token is not text")
	}
	token := string(plain)
	if token == "" || strings.IndexFunc(token, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) >= 0 {
		return "", authError("the sign-in token is not in the expected format")
	}
	return token, nil
}

// ssoCallback is what the browser called back with.
type ssoCallback struct {
	// blob is the base64 blob out of the path.
	blob string
	// redirect is the return= URL to send the browser on to, "" when it
	// gave none.
	redirect string
}

// parseRequestLine reads a callback out of an HTTP request line, or
// false for anything that is not the callback — the port sees stray
// connections, and answering them as the sign-in would abandon the wait.
func parseRequestLine(line string) (ssoCallback, bool) {
	parts := strings.Fields(line)
	if len(parts) < 3 || parts[0] != "GET" || !strings.HasPrefix(parts[2], "HTTP/1.") {
		return ssoCallback{}, false
	}
	path, ok := strings.CutPrefix(parts[1], ssoCallbackPath)
	if !ok {
		return ssoCallback{}, false
	}
	blob, query, hasQuery := strings.Cut(path, "?")
	if blob == "" {
		return ssoCallback{}, false
	}
	callback := ssoCallback{blob: pathDecode(blob)}
	if hasQuery {
		for p := range strings.SplitSeq(query, "&") {
			if value, ok := strings.CutPrefix(p, "return="); ok {
				callback.redirect = pathDecode(value)
				break
			}
		}
	}
	return callback, true
}

// pathDecode resolves %XX escapes. + is left alone: it is a valid base64
// character and this is a path, not a form body. Invalid UTF-8 keeps the
// value as it came.
func pathDecode(value string) string {
	decoded := fileuri.Decode(value)
	if !utf8.Valid(decoded) {
		return value
	}
	return string(decoded)
}

const (
	ssoNotFound = "HTTP/1.1 404 Not Found\r\nConnection: close\r\nContent-Type: text/html\r\nContent-Length: 0\r\n\r\n"
	ssoSuccess  = "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: text/html\r\n\r\n" +
		"<html><title>Signed in</title><body>You can close this tab.</body></html>\r\n"
)

// awaitSSOToken opens url in the browser and waits for the IdP to come
// back on the loopback port, answering stray requests 404 and the
// callback with the redirect (or a success page).
func awaitSSOToken(ctx context.Context, url string, timeout time.Duration) (string, error) {
	// Bound before opening the browser: the IdP redirect can be quick,
	// and a connection refused there loses the sign-in.
	listener, err := net.Listen("tcp", ssoCallbackAddr)
	if err != nil {
		return "", authError("cannot listen on port 29786 for the browser sign-in: " + err.Error())
	}
	defer func() { _ = listener.Close() }()

	if err := openBrowser(url); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return "", ssoWaitEnded(ctx)
			}
			return "", authError("the browser sign-in connection failed: " + err.Error())
		}
		blob, ok := answerSSOConnection(conn)
		if ok {
			return blob, nil
		}
	}
}

// answerSSOConnection reads one request off the loopback port and
// answers it, reporting the blob when it was the callback.
func answerSSOConnection(conn net.Conn) (string, bool) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	callback, ok := parseRequestLine(strings.TrimRight(line, "\r\n"))
	if !ok {
		_, _ = conn.Write([]byte(ssoNotFound))
		return "", false
	}
	response := ssoSuccess
	if callback.redirect != "" {
		response = "HTTP/1.1 302 Found\r\nConnection: close\r\nContent-Length: 0\r\nLocation: " +
			callback.redirect + "\r\n\r\n"
	}
	_, _ = conn.Write([]byte(response))
	return callback.blob, true
}

// ssoWaitEnded is the error for a browser sign-in whose wait ran out:
// the timeout, or the caller withdrawing the request.
func ssoWaitEnded(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return authError("the browser sign-in was not completed in time")
	}
	return incomplete("the browser sign-in was withdrawn")
}

// openBrowser hands a URL to the desktop's browser. A variable so tests
// can play the browser.
var openBrowser = openInBrowser

// openInBrowser runs xdg-open rather than a configured browser: the
// sign-in has to land in the browser the user is already signed into
// their IdP with.
func openInBrowser(url string) error {
	cmd := exec.Command("xdg-open", url) //nolint:gosec // the URL is vetted by the caller
	if err := cmd.Start(); err != nil {
		log.Printf("vpn: cannot open a browser for the VPN sign-in: %v", err)
		return authError("cannot open a browser to sign in with")
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ssoBase64Decode decodes standard base64, tolerating a blob that
// arrives unpadded or wrapped.
func ssoBase64Decode(value string) ([]byte, bool) {
	trimmed := stripSpace(value)
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return decoded, true
	}
	decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(trimmed, "="))
	if err != nil {
		return nil, false
	}
	return decoded, true
}
