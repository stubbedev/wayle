package openconnect

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBase64RoundTripsIncludingThePaddedLengths(t *testing.T) {
	for _, raw := range [][]byte{{}, {0}, {0, 1}, {0, 1, 2}, bytes.Repeat([]byte{255}, 65)} {
		encoded := base64.StdEncoding.EncodeToString(raw)
		if got, ok := ssoBase64Decode(encoded); !ok || !bytes.Equal(got, raw) {
			t.Errorf("decode(%q) = %x, %v", encoded, got, ok)
		}
		// Unpadded, the way some IdPs pass it through a path.
		if got, ok := ssoBase64Decode(strings.TrimRight(encoded, "=")); !ok || !bytes.Equal(got, raw) {
			t.Errorf("unpadded decode(%q) = %x, %v", encoded, got, ok)
		}
	}
	if got := base64.StdEncoding.EncodeToString([]byte("any carnal pleasure.")); got != "YW55IGNhcm5hbCBwbGVhc3VyZS4=" {
		t.Errorf("encoded = %q", got)
	}
	if _, ok := ssoBase64Decode("not base64!"); ok {
		t.Error("garbage decoded")
	}
}

func TestAPublicKeyRoundTripsThroughItsDERWrapper(t *testing.T) {
	point := append([]byte{0x04}, bytes.Repeat([]byte{7}, 64)...)
	der := spki(point)
	if len(der) != len(p256SPKIPrefix)+p256PointLen {
		t.Fatalf("der length = %d", len(der))
	}
	if got, ok := pointFromSPKI(der); !ok || !bytes.Equal(got, point) {
		t.Errorf("point = %x, %v", got, ok)
	}
}

func TestAKeyThatIsNotAP256PointIsRefused(t *testing.T) {
	// A different curve derives a different secret, so accepting one
	// would surface as an unexplained decryption failure.
	compressed := spki(bytes.Repeat([]byte{0x02}, p256PointLen))
	for _, der := range [][]byte{nil, p256SPKIPrefix, compressed, compressed[:len(compressed)-1]} {
		if _, ok := pointFromSPKI(der); ok {
			t.Errorf("pointFromSPKI(%x) accepted", der)
		}
	}
}

// blobBytes lays a blob out the way hpke.c documents it.
func blobBytes(pubkey, tag, ciphertext, iv []byte) []byte {
	out := []byte{0x00, 0x01}
	for _, field := range []struct {
		tag   uint16
		value []byte
	}{{tagPubkey, pubkey}, {tagAEADTag, tag}, {tagCiphertext, ciphertext}, {tagIV, iv}} {
		out = binary.BigEndian.AppendUint16(out, field.tag)
		out = binary.BigEndian.AppendUint16(out, uint16(len(field.value)))
		out = append(out, field.value...)
	}
	return out
}

func TestABlobDecodesIntoItsFourFields(t *testing.T) {
	blob, err := parseBlob(blobBytes(bytes.Repeat([]byte{1}, 91), bytes.Repeat([]byte{2}, 12),
		bytes.Repeat([]byte{3}, 20), bytes.Repeat([]byte{4}, 12)))
	if err != nil {
		t.Fatal(err)
	}
	if len(blob.pubkey) != 91 || !bytes.Equal(blob.tag, bytes.Repeat([]byte{2}, 12)) ||
		!bytes.Equal(blob.ciphertext, bytes.Repeat([]byte{3}, 20)) || !bytes.Equal(blob.iv, bytes.Repeat([]byte{4}, 12)) {
		t.Errorf("blob = %+v", blob)
	}
}

func TestAMalformedBlobSaysSoRatherThanFailingToDecryptLater(t *testing.T) {
	unknown := binary.BigEndian.AppendUint16([]byte{0x00, 0x01}, 9)
	unknown = append(binary.BigEndian.AppendUint16(unknown, 1), 0)
	partial := binary.BigEndian.AppendUint16([]byte{0x00, 0x01}, tagPubkey)
	partial = append(binary.BigEndian.AppendUint16(partial, 1), 0)
	repeated := blobBytes(bytes.Repeat([]byte{1}, 91), bytes.Repeat([]byte{2}, 12), bytes.Repeat([]byte{3}, 20), bytes.Repeat([]byte{4}, 12))
	repeated = binary.BigEndian.AppendUint16(repeated, tagIV)
	repeated = append(binary.BigEndian.AppendUint16(repeated, 12), bytes.Repeat([]byte{5}, 12)...)

	for name, raw := range map[string][]byte{
		"no leading 0x0001":      {0x00, 0x02, 0x00, 0x01},
		"empty":                  nil,
		"a length past the end":  {0x00, 0x01, 0x00, 0x01, 0xff, 0xff, 0x00},
		"a tag nobody defined":   unknown,
		"a missing field":        partial,
		"the wrong nonce size":   blobBytes(bytes.Repeat([]byte{1}, 91), bytes.Repeat([]byte{2}, 12), bytes.Repeat([]byte{3}, 20), bytes.Repeat([]byte{4}, 8)),
		"a repeated field":       repeated,
		"a header with no value": {0x00, 0x01, 0x00},
	} {
		if _, err := parseBlob(raw); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// sealAsGateway seals a token exactly as the gateway does, so the whole
// key schedule is exercised: ECDH, HKDF with AC_ECIES, AES-256-GCM with
// the twelve-byte tag.
func sealAsGateway(t *testing.T, clientPublicDER []byte, token string) tokenBlob {
	t.Helper()
	gatewayKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	point, ok := pointFromSPKI(clientPublicDER)
	if !ok {
		t.Fatal("the client's key is not an SPKI")
	}
	clientKey, err := ecdh.P256().NewPublicKey(point)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := gatewayKey.ECDH(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hkdf.Key(sha256.New, secret, nil, hkdfInfo, 32)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := newGCM12(key)
	if err != nil {
		t.Fatal(err)
	}
	iv := bytes.Repeat([]byte{7}, ssoIVLen)
	sealed := gcm.Seal(nil, iv, []byte(token), nil)
	cut := len(sealed) - ssoTagLen
	return tokenBlob{pubkey: spki(gatewayKey.PublicKey().Bytes()), tag: sealed[cut:], ciphertext: sealed[:cut], iv: iv}
}

func clientDER(t *testing.T, keys *ssoKeys) []byte {
	t.Helper()
	der, ok := ssoBase64Decode(keys.publicBase64)
	if !ok {
		t.Fatal("the public key is not base64")
	}
	return der
}

func mustKeys(t *testing.T) *ssoKeys {
	t.Helper()
	keys, err := generateSSOKeys()
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestATokenSealedTheWayTheGatewaySealsItOpens(t *testing.T) {
	// Pins the key schedule end to end: a wrong info string, salt, curve
	// or tag length fails it.
	keys := mustKeys(t)
	token, err := decryptToken(keys, sealAsGateway(t, clientDER(t, keys), "SSOTOKEN123abc"))
	if err != nil || token != "SSOTOKEN123abc" {
		t.Errorf("token = %q, %v", token, err)
	}
}

func TestATamperedTokenDoesNotOpen(t *testing.T) {
	keys := mustKeys(t)
	blob := sealAsGateway(t, clientDER(t, keys), "SSOTOKEN123abc")
	blob.ciphertext[0] ^= 0xff
	if _, err := decryptToken(keys, blob); err == nil || !strings.Contains(err.Error(), "did not verify") {
		t.Errorf("err = %v", err)
	}
}

func TestATokenSealedForSomeoneElseDoesNotOpen(t *testing.T) {
	// Proves the ECDH binds the token to our key rather than the key
	// material coming from somewhere constant.
	ours, theirs := mustKeys(t), mustKeys(t)
	if _, err := decryptToken(ours, sealAsGateway(t, clientDER(t, theirs), "SSOTOKEN123abc")); err == nil {
		t.Error("another client's token opened")
	}
}

func TestAPlaintextThatIsNotATokenIsRefused(t *testing.T) {
	// openconnect's check: a wrong key must not read as a working sign-in.
	keys := mustKeys(t)
	if _, err := decryptToken(keys, sealAsGateway(t, clientDER(t, keys), "not a token!")); err == nil {
		t.Error("a non-alphanumeric token was accepted")
	}
}

func TestEverySignInGetsItsOwnKey(t *testing.T) {
	first, second := mustKeys(t), mustKeys(t)
	if first.publicBase64 == second.publicBase64 {
		t.Error("two sign-ins share a key")
	}
	// And the wire form is the DER wrapper, not the bare point.
	if _, ok := pointFromSPKI(clientDER(t, first)); !ok {
		t.Error("the public key is not an SPKI")
	}
}

func TestTheCallbackRequestLineYieldsTheBlobAndTheRedirect(t *testing.T) {
	callback, ok := parseRequestLine("GET /api/sso/YWJj?return=https%3A%2F%2Fvpn%2Fdone HTTP/1.1")
	if !ok || callback.blob != "YWJj" || callback.redirect != "https://vpn/done" {
		t.Errorf("callback = %+v, %v", callback, ok)
	}
	// No return= is allowed; the browser just gets the success page.
	bare, ok := parseRequestLine("GET /api/sso/YWJj HTTP/1.0")
	if !ok || bare.blob != "YWJj" || bare.redirect != "" {
		t.Errorf("callback = %+v, %v", bare, ok)
	}
}

func TestAStrayRequestOnThePortIsNotMistakenForTheSignIn(t *testing.T) {
	for _, line := range []string{
		"GET / HTTP/1.1",
		"GET /api/sso/ HTTP/1.1",
		"POST /api/sso/YWJj HTTP/1.1",
		"GET /favicon.ico HTTP/1.1",
		"garbage",
		"GET /api/sso/YWJj RTSP/1.0",
	} {
		if callback, ok := parseRequestLine(line); ok {
			t.Errorf("%q read as %+v", line, callback)
		}
	}
}

func TestAPercentEscapedBlobIsDecodedBeforeItIsParsed(t *testing.T) {
	// base64 uses + and /, which arrive escaped in a URL path.
	if callback, _ := parseRequestLine("GET /api/sso/YQ%2Bb%2Fw%3D%3D HTTP/1.1"); callback.blob != "YQ+b/w==" {
		t.Errorf("blob = %q", callback.blob)
	}
	// A + is not turned into a space: this is a path, not a form.
	if callback, _ := parseRequestLine("GET /api/sso/YQ+b HTTP/1.1"); callback.blob != "YQ+b" {
		t.Errorf("blob = %q", callback.blob)
	}
}

// playBrowser swaps the browser for one that calls the loopback port the
// way an IdP redirect would, after a stray probe, and records the URL it
// was opened at.
func playBrowser(t *testing.T, requests ...string) *string {
	t.Helper()
	opened := new(string)
	previous := openBrowser
	openBrowser = func(url string) error {
		*opened = url
		go func() {
			for _, line := range requests {
				conn, err := net.Dial("tcp", ssoCallbackAddr)
				if err != nil {
					return
				}
				fmt.Fprintf(conn, "%s\r\nHost: localhost\r\n\r\n", line)
				_, _ = bufio.NewReader(conn).ReadString('\n')
				_ = conn.Close()
			}
		}()
		return nil
	}
	t.Cleanup(func() { openBrowser = previous })
	return opened
}

// freeCallbackAddr points the SSO listener at a free loopback port.
func freeCallbackAddr(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	previous := ssoCallbackAddr
	ssoCallbackAddr = addr
	t.Cleanup(func() { ssoCallbackAddr = previous })
}

func TestTheBrowserCallbackIsTakenOffTheLoopbackPortPastStrayProbes(t *testing.T) {
	freeCallbackAddr(t)
	opened := playBrowser(t, "GET /favicon.ico HTTP/1.1", "GET /api/sso/QkxPQg%3D%3D?return=https%3A%2F%2Fvpn%2Fdone HTTP/1.1")
	blob, err := awaitSSOToken(context.Background(), "https://idp.example.com/sso", 5*time.Second)
	if err != nil || blob != "QkxPQg==" {
		t.Fatalf("blob = %q, %v", blob, err)
	}
	if *opened != "https://idp.example.com/sso" {
		t.Errorf("opened %q", *opened)
	}
}

func TestTheCallbackSendsTheBrowserOnToTheGatewaysPage(t *testing.T) {
	for redirect, want := range map[string]string{
		"GET /api/sso/QkxPQg HTTP/1.1":                          "HTTP/1.1 200 OK",
		"GET /api/sso/QkxPQg?return=https%3A%2F%2Fvpn HTTP/1.1": "HTTP/1.1 302 Found",
	} {
		server, clientEnd := net.Pipe()
		done := make(chan string, 1)
		go func() {
			fmt.Fprintf(clientEnd, "%s\r\n\r\n", redirect)
			resp, err := http.ReadResponse(bufio.NewReader(clientEnd), nil)
			if err != nil {
				done <- err.Error()
				return
			}
			_ = resp.Body.Close()
			done <- resp.Proto + " " + resp.Status + " " + resp.Header.Get("Location")
		}()
		if blob, ok := answerSSOConnection(server); !ok || blob != "QkxPQg" {
			t.Errorf("%q: blob = %q, %v", redirect, blob, ok)
		}
		if got := <-done; !strings.HasPrefix(got, want) {
			t.Errorf("%q: answered %q, want %q", redirect, got, want)
		}
	}
}

func TestABrowserThatNeverComesBackTimesOut(t *testing.T) {
	freeCallbackAddr(t)
	playBrowser(t)
	_, err := awaitSSOToken(context.Background(), "https://idp.example.com/sso", 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "not completed in time") {
		t.Errorf("err = %v", err)
	}
}

func TestABrowserThatCannotBeOpenedFailsBeforeAnyWait(t *testing.T) {
	freeCallbackAddr(t)
	previous := openBrowser
	openBrowser = func(string) error { return authError("cannot open a browser to sign in with") }
	t.Cleanup(func() { openBrowser = previous })
	start := time.Now()
	if _, err := awaitSSOToken(context.Background(), "https://idp.example.com/sso", time.Minute); err == nil {
		t.Error("no browser, and yet a sign-in")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("waited for a browser that never opened")
	}
}
