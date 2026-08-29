//! Per-profile credential cache for the openconnect family.
//!
//! Two things are kept, both keyed by NM profile UUID:
//!
//! - the **session cookie**, so a reconnect after a dropped tunnel or a resume
//!   from suspend does not mean another 2FA prompt. This is the whole reason
//!   the old shell helper existed;
//! - the **password**, so the only thing a routine connect asks for is the
//!   second factor.
//!
//! Neither belongs in NetworkManager's store: they are wayle's own artifacts
//! in a shape no other NM client can interpret — the openconnect plugin's own
//! secrets are the cookie's *derivatives*, not these. They live under the
//! user's state directory, created `0700`, written `0600` through the open
//! mode rather than a `chmod` afterwards, so there is no window in which a
//! session cookie is world-readable.

use std::{
    fs,
    io::Write,
    os::unix::fs::{DirBuilderExt, OpenOptionsExt},
    path::PathBuf,
};

use tracing::{debug, warn};

use super::Session;

/// `$XDG_STATE_HOME/wayle/vpn`, or the default state directory under `$HOME`.
fn directory() -> Option<PathBuf> {
    let base = std::env::var_os("XDG_STATE_HOME")
        .map(PathBuf::from)
        .filter(|path| path.is_absolute())
        .or_else(|| {
            std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".local/state"))
        })?;
    Some(base.join("wayle/vpn"))
}

fn path(uuid: &str, extension: &str) -> Option<PathBuf> {
    // A UUID is the only thing that reaches this, but it arrives from NM's
    // dictionary rather than from a type that guarantees its shape, and a
    // stray separator would write outside the state directory.
    if uuid.is_empty() || uuid.contains(['/', '\\']) || uuid.contains("..") {
        return None;
    }
    Some(directory()?.join(format!("{uuid}.{extension}")))
}

fn write_private(uuid: &str, extension: &str, contents: &str) {
    let Some(path) = path(uuid, extension) else {
        return;
    };
    let Some(parent) = path.parent() else {
        return;
    };
    if let Err(error) = fs::DirBuilder::new()
        .recursive(true)
        .mode(0o700)
        .create(parent)
    {
        warn!(%error, "cannot create the VPN credential directory");
        return;
    }

    let written = fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(&path)
        .and_then(|mut file| file.write_all(contents.as_bytes()));
    if let Err(error) = written {
        warn!(%error, "cannot write VPN credentials");
    }
}

fn read(uuid: &str, extension: &str) -> Option<String> {
    fs::read_to_string(path(uuid, extension)?).ok()
}

fn remove(uuid: &str, extension: &str) {
    if let Some(path) = path(uuid, extension) {
        let _ = fs::remove_file(path);
    }
}

/// The cached session for a profile, if one was stored.
pub(super) fn session(uuid: &str) -> Option<Session> {
    let raw = read(uuid, "cookie")?;
    parse_session(&raw)
}

/// Stores a session for reuse on the next connect.
pub(super) fn store_session(uuid: &str, session: &Session) {
    debug!(uuid, "caching VPN session cookie");
    write_private(
        uuid,
        "cookie",
        &format!("{}\n{}\n{}\n", session.cookie, session.host, session.gwcert),
    );
}

/// Drops a cached session — the gateway rejected it, or the user asked for a
/// fresh sign-in.
pub(super) fn forget_session(uuid: &str) {
    remove(uuid, "cookie");
}

/// The cached password for a profile.
pub(super) fn password(uuid: &str) -> Option<String> {
    read(uuid, "password").map(|raw| raw.trim_end_matches('\n').to_owned())
}

/// Stores a password that authenticated successfully.
pub(super) fn store_password(uuid: &str, password: &str) {
    write_private(uuid, "password", password);
}

/// Drops a stored password NM has told us was rejected.
pub(super) fn forget_password(uuid: &str) {
    remove(uuid, "password");
}

/// Reads the three-line cookie file back.
///
/// A file written before the certificate pin was part of a session has two
/// lines, and reads as no session at all: the plugin refuses a secret set
/// without a `gwcert`, so reusing one would fail the activation with nothing
/// to show for it. Signing in again writes the current shape.
fn parse_session(raw: &str) -> Option<Session> {
    let mut lines = raw.lines();
    let cookie = lines.next()?.trim();
    let host = lines.next()?.trim();
    let gwcert = lines.next()?.trim();
    if cookie.is_empty() || host.is_empty() || gwcert.is_empty() {
        return None;
    }
    Some(Session {
        cookie: String::from(cookie),
        host: String::from(host),
        gwcert: String::from(gwcert),
    })
}

#[cfg(test)]
// The state directory is read from the environment, so one test sets it.
#[allow(unsafe_code)]
mod tests {
    use super::*;

    #[test]
    fn a_stored_session_round_trips() {
        let session =
            parse_session("authcookie=abc&user=alice\nvpn.example.com\npin-sha256:AAAA\n")
                .expect("a complete file parses");
        assert_eq!(session.cookie, "authcookie=abc&user=alice");
        assert_eq!(session.host, "vpn.example.com");
        assert_eq!(session.gwcert, "pin-sha256:AAAA");
    }

    #[test]
    fn a_truncated_or_empty_file_is_no_session_rather_than_a_broken_one() {
        // Handing a half-written cookie to the plugin fails at connect time
        // with no useful message; treating it as absent re-authenticates.
        assert_eq!(parse_session("authcookie=abc\n"), None);
        assert_eq!(parse_session("\nvpn.example.com\npin-sha256:AAAA\n"), None);
        assert_eq!(parse_session(""), None);
    }

    #[test]
    fn a_session_cached_before_certificate_pinning_is_not_reused() {
        // Two lines is the old shape. It would produce a secret set with no
        // `gwcert`, which the plugin rejects without ever starting.
        assert_eq!(parse_session("authcookie=abc\nvpn.example.com\n"), None);
    }

    #[test]
    fn a_uuid_that_could_escape_the_directory_is_refused() {
        assert!(path("../../etc/passwd", "cookie").is_none());
        assert!(path("a/b", "cookie").is_none());
        assert!(path("", "cookie").is_none());
    }

    #[test]
    fn forgetting_a_profile_removes_both_files_from_disk() {
        // Deleting a VPN used to leave its session cookie and its password
        // behind, under a UUID nothing would ever look up again.
        let base = std::env::temp_dir().join(format!("wayle-vpn-cache-{}", std::process::id()));
        // SAFETY: the test runner gives every test its own process, and
        // nothing else in this crate reads `XDG_STATE_HOME`.
        unsafe { std::env::set_var("XDG_STATE_HOME", &base) };

        let uuid = "cache-round-trip";
        let stored = Session {
            cookie: String::from("authcookie=abc"),
            host: String::from("vpn.example.com"),
            gwcert: String::from("pin-sha256:AAAA"),
        };
        store_session(uuid, &stored);
        store_password(uuid, "hunter2");
        assert_eq!(session(uuid).as_ref(), Some(&stored));
        assert_eq!(password(uuid).as_deref(), Some("hunter2"));

        forget_session(uuid);
        forget_password(uuid);
        assert_eq!(session(uuid), None);
        assert_eq!(password(uuid), None);
        assert!(
            !path(uuid, "cookie").is_some_and(|path| path.exists()),
            "the cookie file survived"
        );
        assert!(
            !path(uuid, "password").is_some_and(|path| path.exists()),
            "the password file survived"
        );

        let _ = fs::remove_dir_all(&base);
    }

    #[test]
    fn a_real_uuid_lands_inside_the_state_directory() {
        // Only meaningful where a state directory resolves at all; the
        // rejection cases above are what this pairs with.
        let Some(path) = path("6a1c-uuid", "cookie") else {
            return;
        };
        assert!(path.ends_with("wayle/vpn/6a1c-uuid.cookie"), "{path:?}");
    }
}
