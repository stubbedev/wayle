package openconnect

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"net/http"
)

// The gateway certificate pin the openconnect plugin asks for as gwcert
// (cert.rs).
//
// nm-openconnect passes that secret straight to openconnect
// --servercert, and the plugin's need_secrets counts the key as missing
// until it is there — so a sign-in that returns only a cookie and a
// gateway never launches openconnect at all, however valid the cookie.
//
// Of the three forms --servercert accepts, only pin-sha256: is a hash
// of the certificate itself; sha1:/sha256: are hex hashes of the public
// key, which is why hashing the whole DER and prefixing it sha256: is
// rejected. The pin is RFC 7469's: SHA-256 over the certificate's whole
// encoded SubjectPublicKeyInfo. The Rust walks the DER by hand to find
// it; crypto/x509 hands it over as RawSubjectPublicKeyInfo.

// certPin is the pin-sha256: fingerprint of a DER-encoded certificate,
// or false when the bytes are not a certificate this can read.
func certPin(der []byte) (string, bool) {
	cert, err := x509.ParseCertificate(der)
	if err != nil || len(cert.RawSubjectPublicKeyInfo) == 0 {
		return "", false
	}
	digest := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "pin-sha256:" + base64.StdEncoding.EncodeToString(digest[:]), true
}

// peerPin is the certificate pin of whoever answered this response, or
// false on a response with no verified TLS peer — a pin invented for a
// connection nobody verified would be worse than no sign-in at all.
func peerPin(resp *http.Response) (string, bool) {
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return "", false
	}
	return certPin(resp.TLS.PeerCertificates[0].Raw)
}
