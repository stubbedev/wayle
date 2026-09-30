package openconnect

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"os"
	"testing"
)

// certificateFixture is a self-signed P-256 certificate for
// vpn.example.com; expectedPin is what `openssl x509 -pubkey | openssl
// pkey -pubin -outform der | sha256 | base64` computes for it, and what
// openconnect 9.12 accepted for the same certificate.
const certificateFixture = "MIIBiDCCAS+gAwIBAgIULWz/JZGl3ygYikhOjj+qjSL0fc4wCgYIKoZIzj0EAwIwGjEYMBYGA1UE" +
	"AwwPdnBuLmV4YW1wbGUuY29tMB4XDTI2MDgyOTEyMTAyNVoXDTM2MDgyNjEyMTAyNVowGjEYMBYGA1UE" +
	"AwwPdnBuLmV4YW1wbGUuY29tMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEy4iFnIEy5Fr+/ZDgGzRe" +
	"DOqAzwtiFcfALwjht8WwwIJA7bjPRdw5+kuOo0xTLWaVklIzFnlsFpk+xMQw0TGvzKNTMFEwHQYDVR0O" +
	"BBYEFH9KHDQLlJ7mIv7F0EfaQmn+deh2MB8GA1UdIwQYMBaAFH9KHDQLlJ7mIv7F0EfaQmn+deh2MA8G" +
	"A1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDRwAwRAIgESQS805DfMcBRZMifgXqNsrxxxtH3sxb4nIE" +
	"scoQTQUCIHnxw4VWTcHPaqkPkBGCZgBaw+++iXkzvT86Ff+q0anr"

const expectedPin = "pin-sha256:1AGrIgWOEB5Sxfg6xFFl5UEsy9ForMLzaTiwiRHc+Hw="

func fixtureDER(t *testing.T) []byte {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(certificateFixture)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestACertificatePinsToTheHashOpenconnectPrints(t *testing.T) {
	if got, ok := certPin(fixtureDER(t)); !ok || got != expectedPin {
		t.Errorf("pin = %q, %v; want %q", got, ok, expectedPin)
	}
}

func TestTheFakeGatewaysCertificatePinsToTheCommittedConstant(t *testing.T) {
	raw, err := os.ReadFile("testdata/gateway.crt")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if got, ok := certPin(block.Bytes); !ok || got != fakePin {
		t.Errorf("pin = %q, %v; want %q", got, ok, fakePin)
	}
}

func TestThePinIsOfThePublicKeyNotOfTheWholeCertificate(t *testing.T) {
	// The whole-DER digest is what sha256: would have meant, and it is
	// not what openconnect compares a pin-sha256: against.
	der := fixtureDER(t)
	whole := sha256.Sum256(der)
	if got, _ := certPin(der); got == "pin-sha256:"+base64.StdEncoding.EncodeToString(whole[:]) {
		t.Error("pinned the whole certificate")
	}
}

func TestBytesThatAreNotACertificateProduceNoPin(t *testing.T) {
	// Silently pinning garbage would hand the plugin a value that only
	// fails once the tunnel is being brought up.
	for _, der := range [][]byte{nil, []byte("not der at all"), {0x30, 0x02, 0x01, 0x00}, {0x30, 0x82, 0xff, 0xff, 0x00}} {
		if got, ok := certPin(der); ok {
			t.Errorf("pin(%x) = %q", der, got)
		}
	}
}

func TestATruncatedCertificateIsRefusedRatherThanReadShort(t *testing.T) {
	der := fixtureDER(t)
	for _, cut := range []int{len(der) / 2, len(der) - 1} {
		if got, ok := certPin(der[:cut]); ok {
			t.Errorf("truncated at %d: pin = %q", cut, got)
		}
	}
}
