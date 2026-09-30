package openconnect

// What the NM secret agent's tests need to set up a profile that has
// signed in before (the Rust's openconnect::testing). They go through
// the same cache a sign-in writes, under $XDG_STATE_HOME/wayle/vpn, so
// a test sets XDG_STATE_HOME to a temporary directory first.

// RememberPassword stores a password the way a sign-in that worked does.
func RememberPassword(uuid, password string) { storePassword(uuid, password) }

// RememberedPassword is the password stored for a profile, and whether
// there is one.
func RememberedPassword(uuid string) (string, bool) { return cachedPassword(uuid) }
