package portal

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// secretLen is the master secret's length, as the reference backends.
const secretLen = 64

// secret is org.freedesktop.impl.portal.Secret (secret.rs): a stable
// per-app master secret libsecret derives the app's keyring key from,
// generated on first use and kept at
// $XDG_DATA_HOME/wayle/portal/secrets/<app_id> (0600). It must never
// change, or the keyring becomes undecryptable.
type secret struct{}

func secretIface() dbusx.Interface {
	return dbusx.Interface{
		Name:       "org.freedesktop.impl.portal.Secret",
		Methods:    secret{},
		Properties: dbusx.Getters{"version": func() any { return uint32(1) }},
	}
}

// RetrieveSecret writes the app's secret to fd.
func (secret) RetrieveSecret(_ dbus.ObjectPath, appID string, fd dbus.UnixFD, _ Vardict) (uint32, Vardict, *dbus.Error) {
	f := os.NewFile(uintptr(fd), "secret")
	defer func() { _ = f.Close() }()
	value, err := loadOrCreateSecret(appID)
	if err != nil {
		warnf("secret: could not obtain the master secret for %s: %v", appID, err)
		return ResponseOther, Vardict{}, nil
	}
	if _, err := f.Write(value); err != nil {
		warnf("secret: writing to fd failed for %s: %v", appID, err)
		return ResponseOther, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{}, nil
}

// loadOrCreateSecret reads the stored secret, or generates and stores
// a fresh one when there is none of the right length.
func loadOrCreateSecret(appID string) ([]byte, error) {
	path, err := secretPath(appID)
	if err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(path); err == nil && len(b) == secretLen { //nolint:gosec // a sanitized name under the data dir
		return b, nil
	}
	value := make([]byte, secretLen)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		return nil, err
	}
	return value, nil
}

// secretPath is the app's file under $XDG_DATA_HOME (when absolute) or
// ~/.local/share.
func secretPath(appID string) (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(base) {
		home, ok := os.LookupEnv("HOME")
		if !ok {
			return "", errors.New("neither XDG_DATA_HOME nor HOME is set")
		}
		base = filepath.Join(home, ".local/share")
	}
	return filepath.Join(base, "wayle/portal/secrets", sanitizeAppID(appID)), nil
}

// sanitizeAppID maps an app id to one safe path component: anything
// but ASCII alphanumerics and .-_ becomes _, and a leading . (or an
// empty id) is prefixed with _, so it is never ".", ".." or hidden.
func sanitizeAppID(appID string) string {
	var b strings.Builder
	b.WriteByte('_')
	for _, r := range appID {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(".-_", r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 1 && out[1] != '.' {
		return out[1:]
	}
	return out
}
