package network

import (
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// This file holds the two WireGuard helpers the form leans on: key
// generation (vpn/wg_keys.rs) and wg-quick import (vpn/wg_quick.rs).

// KeyPair is a freshly generated private key and the public key derived
// from it, both base64 as WireGuard writes them.
type KeyPair struct {
	// Private is this end's private-key.
	Private string
	// Public is what to hand whoever runs the other end.
	Public string
}

// GenerateKeyPair makes a new WireGuard key pair from the OS random
// source. Keys are X25519 in WireGuard's encoding: 32 raw bytes,
// standard base64 with padding, which is what wg prints and NM stores.
func GenerateKeyPair() KeyPair {
	var private [32]byte
	// crypto/rand.Read never fails on Linux (it aborts the process on an
	// unusable random source instead).
	_, _ = rand.Read(private[:])
	public, _ := curve25519.X25519(private[:], curve25519.Basepoint)
	return KeyPair{
		Private: base64.StdEncoding.EncodeToString(private[:]),
		Public:  base64.StdEncoding.EncodeToString(public),
	}
}

// PublicKeyFor derives the public key for a base64 private key. ok is
// false when the text is not a 32-byte base64 key, so a half-typed or
// pasted-wrong key shows nothing rather than someone else's public key.
func PublicKeyFor(private string) (public string, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(private))
	if err != nil || len(raw) != curve25519.ScalarSize {
		return "", false
	}
	derived, err := curve25519.X25519(raw, curve25519.Basepoint)
	if err != nil {
		return "", false
	}
	return base64.StdEncoding.EncodeToString(derived), true
}

// defaultInterface is the interface name when the file implies none.
const defaultInterface = "wg0"

// ParseWgQuick parses a wg-quick file into the WireGuard form's values.
// fileName names the interface the way `wg-quick up wg0` does (a
// wg0.conf is wg0); pass "" when there is no file. ok is false when the
// text is not a wg-quick config: an [Interface] with a private key is
// the least it can be. Only the keys the form asks for are read; MTU,
// Table, PreUp and friends have nowhere to go in the profile, and only
// the first [Peer] is taken, since the form models one.
func ParseWgQuick(text, fileName string) (values map[string]string, ok bool) {
	iface := map[string]string{}
	peer := map[string]string{}
	var section map[string]string
	seenPeer := false
	for line := range strings.Lines(text) {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if name, isSection := strings.CutPrefix(line, "["); isSection && strings.HasSuffix(name, "]") {
			switch strings.ToLower(strings.TrimSuffix(name, "]")) {
			case "interface":
				section = iface
			case "peer":
				section = nil
				if !seenPeer {
					seenPeer = true
					section = peer
				}
			default:
				section = nil
			}
			continue
		}
		key, value, found := strings.Cut(line, "=")
		value = strings.TrimSpace(value)
		if !found || value == "" || section == nil {
			continue
		}
		section[strings.ToLower(strings.TrimSpace(key))] = value
	}
	if _, has := iface["privatekey"]; !has {
		return nil, false
	}
	values = map[string]string{"interface": interfaceName(fileName)}
	for key, from := range map[string]struct {
		section map[string]string
		key     string
	}{
		"private-key":        {iface, "privatekey"},
		"address":            {iface, "address"},
		"dns":                {iface, "dns"},
		"peer-public-key":    {peer, "publickey"},
		"peer-preshared-key": {peer, "presharedkey"},
		"peer-allowed-ips":   {peer, "allowedips"},
		"peer-endpoint":      {peer, "endpoint"},
		"peer-keepalive":     {peer, "persistentkeepalive"},
	} {
		if value, has := from.section[from.key]; has {
			values[key] = value
		}
	}
	return values, true
}

// interfaceName takes wg0 out of wg0.conf; a name the kernel would
// refuse (empty, over 15 characters, odd characters) is not used.
func interfaceName(fileName string) string {
	stem := strings.TrimSuffix(filepath.Base(fileName), ".conf")
	if fileName == "" || stem == "" || len(stem) > 15 {
		return defaultInterface
	}
	for _, c := range stem {
		if !isASCIIAlnum(c) && c != '-' && c != '_' {
			return defaultInterface
		}
	}
	return stem
}
