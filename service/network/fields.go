package network

import (
	"slices"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// This file turns "NM wants secrets for setting X, hinting at keys Y"
// into a prompt (agent/fields.rs). The hints are the point of the
// VPN-hints capability: a plugin says exactly which keys it is missing,
// so the prompt asks for those rather than guessing that every VPN
// wants a username and a password.

// machineOnly are keys no human can answer. OpenConnect's secrets are a
// session cookie, the gateway it was issued for, and that gateway's
// certificate hash: the output of an authentication, not its input.
var machineOnly = []string{
	"cookie",
	"gwcert",
	"gateway",
	"resolve",
	"xmlconfig",
	"certsigs",
	"lasthost",
}

// defaultKey is a setting's conventional secret key, used when NM
// hints at nothing.
func defaultKey(setting string) (string, bool) {
	switch setting {
	case "802-11-wireless-security":
		return "psk", true
	case "802-1x", "vpn", "pppoe", "gsm", "cdma":
		return "password", true
	case "wireguard":
		return "private-key", true
	}
	return "", false
}

// describe labels a key and says whether it is masked. An unknown key
// from some plugin's own vocabulary is masked: a secret shown in the
// clear is the worse of the two wrong guesses.
func describe(key string) (label string, secret bool) {
	switch key {
	case "psk", "password", "passwd", "leap-password", "secret":
		return "Password", true
	case "wep-key0", "wep-key1", "wep-key2", "wep-key3":
		return "WEP key", true
	case "private-key-password":
		return "Private key password", true
	case "private-key":
		return "Private key", true
	case "pin":
		return "PIN", true
	case "user", "username", "user-name":
		return "Username", false
	case "usergroup":
		return "Group", false
	case "domain":
		return "Domain", false
	}
	return "Password", true
}

// fieldsFor is what to ask the user for, or nil when there is nothing
// a person could usefully type (for_request).
func fieldsFor(setting string, hints []string) []secrets.Field {
	var keys []string
	if len(hints) == 0 {
		key, ok := defaultKey(setting)
		if !ok {
			return nil
		}
		keys = []string{key}
	} else {
		for _, hint := range hints {
			if !slices.Contains(machineOnly, hint) {
				keys = append(keys, hint)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	fields := make([]secrets.Field, 0, len(keys))
	for _, key := range keys {
		label, secret := describe(key)
		fields = append(fields, secrets.Field{Key: key, Label: label, Secret: secret})
	}
	return fields
}
