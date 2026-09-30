package openconnect

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// The per-profile credential cache for the openconnect family (cache.rs).
//
// Two things are kept, both keyed by NM profile UUID:
//
//   - the session cookie, so a reconnect after a dropped tunnel or a
//     resume from suspend does not mean another 2FA prompt;
//   - the password, so the only thing a routine connect asks for is the
//     second factor.
//
// Neither belongs in NetworkManager's store: they are wayle's own
// artifacts in a shape no other NM client can interpret. They live in
// $XDG_STATE_HOME/wayle/vpn as <uuid>.cookie (three lines: cookie,
// host, gwcert) and <uuid>.password (the password as typed), plain text
// like the Rust's. The directory is created 0700 and the files written
// 0600 through the open mode rather than a chmod afterwards, so there is
// no window in which a session cookie is world-readable.

// cacheDirectory is $XDG_STATE_HOME/wayle/vpn, or the default state
// directory under $HOME. A relative XDG_STATE_HOME is ignored, as the
// XDG spec says.
func cacheDirectory() (string, bool) {
	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home, ok := os.LookupEnv("HOME")
		if !ok {
			return "", false
		}
		base = filepath.Join(home, ".local/state")
	}
	return filepath.Join(base, "wayle/vpn"), true
}

// cachePath is where one of a profile's files lives. A UUID is the only
// thing that reaches this, but it arrives from NM's dictionary rather
// than from a type that guarantees its shape, and a stray separator
// would write outside the state directory.
func cachePath(uuid, extension string) (string, bool) {
	if uuid == "" || strings.ContainsAny(uuid, `/\`) || strings.Contains(uuid, "..") {
		return "", false
	}
	dir, ok := cacheDirectory()
	if !ok {
		return "", false
	}
	return filepath.Join(dir, uuid+"."+extension), true
}

func writePrivate(uuid, extension, contents string) {
	path, ok := cachePath(uuid, extension)
	if !ok {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("vpn: cannot create the VPN credential directory: %v", err)
		return
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // cachePath refuses anything that could leave the state directory
	if err == nil {
		_, err = file.WriteString(contents)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		log.Printf("vpn: cannot write VPN credentials: %v", err)
	}
}

func readCached(uuid, extension string) (string, bool) {
	path, ok := cachePath(uuid, extension)
	if !ok {
		return "", false
	}
	raw, err := os.ReadFile(path) //nolint:gosec // cachePath refuses anything that could leave the state directory
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func removeCached(uuid, extension string) {
	if path, ok := cachePath(uuid, extension); ok {
		_ = os.Remove(path)
	}
}

// cachedSession is the cached session for a profile, if one was stored.
func cachedSession(uuid string) (session, bool) {
	raw, ok := readCached(uuid, "cookie")
	if !ok {
		return session{}, false
	}
	return parseSession(raw)
}

// storeSession caches a session for reuse on the next connect.
func storeSession(uuid string, s session) {
	writePrivate(uuid, "cookie", s.cookie+"\n"+s.host+"\n"+s.gwcert+"\n")
}

// forgetSession drops a cached session — the gateway rejected it, or
// the user asked for a fresh sign-in.
func forgetSession(uuid string) { removeCached(uuid, "cookie") }

// cachedPassword is the cached password for a profile.
func cachedPassword(uuid string) (string, bool) {
	raw, ok := readCached(uuid, "password")
	if !ok {
		return "", false
	}
	return strings.TrimRight(raw, "\n"), true
}

// storePassword stores a password that authenticated successfully.
func storePassword(uuid, password string) { writePrivate(uuid, "password", password) }

// forgetPassword drops a stored password the gateway refused.
func forgetPassword(uuid string) { removeCached(uuid, "password") }

// parseSession reads the three-line cookie file back. A file written
// before the certificate pin was part of a session has two lines, and
// reads as no session at all: the plugin refuses a secret set without a
// gwcert, so reusing one would fail the activation with nothing to show
// for it.
func parseSession(raw string) (session, bool) {
	lines := strings.SplitN(raw, "\n", 4)
	if len(lines) < 3 {
		return session{}, false
	}
	line := func(i int) string { return strings.TrimSpace(strings.TrimSuffix(lines[i], "\r")) }
	s := session{cookie: line(0), host: line(1), gwcert: line(2)}
	if s.cookie == "" || s.host == "" || s.gwcert == "" {
		return session{}, false
	}
	return s, true
}
