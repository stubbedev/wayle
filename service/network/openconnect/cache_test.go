package openconnect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAStoredSessionRoundTrips(t *testing.T) {
	s, ok := parseSession("authcookie=abc&user=alice\nvpn.example.com\npin-sha256:AAAA\n")
	if !ok {
		t.Fatal("a complete file does not parse")
	}
	if s != (session{cookie: "authcookie=abc&user=alice", host: "vpn.example.com", gwcert: "pin-sha256:AAAA"}) {
		t.Errorf("session = %+v", s)
	}
}

func TestATruncatedOrEmptyFileIsNoSessionRatherThanABrokenOne(t *testing.T) {
	// Handing a half-written cookie to the plugin fails at connect time
	// with no useful message; treating it as absent re-authenticates.
	for _, raw := range []string{"authcookie=abc\n", "\nvpn.example.com\npin-sha256:AAAA\n", ""} {
		if s, ok := parseSession(raw); ok {
			t.Errorf("parseSession(%q) = %+v", raw, s)
		}
	}
}

func TestASessionCachedBeforeCertificatePinningIsNotReused(t *testing.T) {
	// Two lines is the old shape: a secret set with no gwcert, which the
	// plugin rejects without ever starting.
	if s, ok := parseSession("authcookie=abc\nvpn.example.com\n"); ok {
		t.Errorf("session = %+v", s)
	}
}

func TestAUUIDThatCouldEscapeTheDirectoryIsRefused(t *testing.T) {
	stateHome(t)
	for _, uuid := range []string{"../../etc/passwd", "a/b", `a\b`, ""} {
		if path, ok := cachePath(uuid, "cookie"); ok {
			t.Errorf("cachePath(%q) = %q", uuid, path)
		}
	}
}

func TestARealUUIDLandsInsideTheStateDirectory(t *testing.T) {
	base := stateHome(t)
	path, ok := cachePath("6a1c-uuid", "cookie")
	if !ok || path != filepath.Join(base, "wayle/vpn/6a1c-uuid.cookie") {
		t.Errorf("path = %q, %v", path, ok)
	}
}

func TestTheStateDirectoryFallsBackToHomeForARelativeXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, xdg := range []string{"", "relative/state"} {
		t.Setenv("XDG_STATE_HOME", xdg)
		if dir, ok := cacheDirectory(); !ok || dir != filepath.Join(home, ".local/state/wayle/vpn") {
			t.Errorf("XDG_STATE_HOME=%q: dir = %q, %v", xdg, dir, ok)
		}
	}
}

func TestCredentialsAreWrittenPrivateAndInTheRustsFormat(t *testing.T) {
	base := stateHome(t)
	storeSession("fmt", session{cookie: "authcookie=abc", host: "vpn.example.com", gwcert: "pin-sha256:AAAA"})
	storePassword("fmt", "hunter2")

	dir := filepath.Join(base, "wayle/vpn")
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, %v", info, err)
	}
	for file, want := range map[string]string{
		"fmt.cookie":   "authcookie=abc\nvpn.example.com\npin-sha256:AAAA\n",
		"fmt.password": "hunter2",
	} {
		path := filepath.Join(dir, file)
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Errorf("%s = %q, %v; want %q", file, raw, err, want)
		}
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", file, info.Mode().Perm())
		}
	}
	// A password file with a trailing newline (written by hand) reads
	// without it.
	if err := os.WriteFile(filepath.Join(dir, "fmt.password"), []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := cachedPassword("fmt"); got != "hunter2" {
		t.Errorf("password = %q", got)
	}
}

func TestForgettingAProfileRemovesBothFilesFromDisk(t *testing.T) {
	// Deleting a VPN used to leave its session cookie and its password
	// behind, under a UUID nothing would ever look up again.
	stateHome(t)
	const uuid = "cache-round-trip"
	stored := session{cookie: "authcookie=abc", host: "vpn.example.com", gwcert: "pin-sha256:AAAA"}
	storeSession(uuid, stored)
	storePassword(uuid, "hunter2")
	if got, ok := cachedSession(uuid); !ok || got != stored {
		t.Fatalf("session = %+v, %v", got, ok)
	}
	if got, ok := cachedPassword(uuid); !ok || got != "hunter2" {
		t.Fatalf("password = %q, %v", got, ok)
	}

	handOut(uuid, stored)
	Forget(uuid)
	if _, ok := cachedSession(uuid); ok {
		t.Error("the session survived")
	}
	if _, ok := cachedPassword(uuid); ok {
		t.Error("the password survived")
	}
	for _, ext := range []string{"cookie", "password"} {
		path, _ := cachePath(uuid, ext)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("the %s file survived: %v", ext, err)
		}
	}
	handedMu.Lock()
	_, handed := handedOut[uuid]
	handedMu.Unlock()
	if handed {
		t.Error("the handed-out record survived")
	}
}

func TestTheTestHelpersGoThroughTheSameCache(t *testing.T) {
	stateHome(t)
	if _, ok := RememberedPassword("helper"); ok {
		t.Fatal("a password before one was remembered")
	}
	RememberPassword("helper", "hunter2")
	if got, ok := cachedPassword("helper"); !ok || got != "hunter2" {
		t.Errorf("cached = %q, %v", got, ok)
	}
	if got, ok := RememberedPassword("helper"); !ok || got != "hunter2" {
		t.Errorf("remembered = %q, %v", got, ok)
	}
}
