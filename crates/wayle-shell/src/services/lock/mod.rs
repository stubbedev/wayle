//! In-process lock-screen bridge.
//!
//! The lock screen lives on the GTK thread as the [`Lock`] component. The
//! `wayle lock` CLI and the `com.wayle.Shell1` `Lock` D-Bus method route here,
//! and the logind signal listener inside the component also drives it. No
//! subprocess is involved.
//!
//! [`Lock`]: crate::shell::lock::Lock

use std::sync::OnceLock;

use relm4::Sender;
use tracing::warn;

use crate::shell::lock::LockInput;

/// GTK-thread sender into the lock component. Set once the shell UI exists.
static LOCK_SENDER: OnceLock<Sender<LockInput>> = OnceLock::new();

/// Records the lock component's input sender. Called once during shell init.
pub(crate) fn register_sender(sender: Sender<LockInput>) {
    if LOCK_SENDER.set(sender).is_err() {
        warn!("lock sender already registered");
    }
}

/// Locks the session. Returns `false` if the shell UI is not ready.
pub(crate) fn lock() -> bool {
    let Some(sender) = LOCK_SENDER.get() else {
        return false;
    };
    sender.emit(LockInput::Lock);
    true
}

/// Claims the once-per-session `lock-on-start` gate. Returns `true` the first
/// time it is called in a login session, `false` on any later call.
///
/// This is what stops `lock-on-start` from relocking a session the user is
/// already in when the shell merely *restarts* — `home-manager switch` bounces
/// wayle.service on a config change, and `Restart=on-failure` restarts it after
/// a crash. Only a genuine session start (greetd autologin) should lock.
///
/// The marker lives under `XDG_RUNTIME_DIR`, which logind clears when the login
/// session ends, so the next real login locks again. With no runtime dir we
/// cannot dedupe, so we lock — failing secure for an access gate.
pub(crate) fn claim_lock_on_start() -> bool {
    match std::env::var_os("XDG_RUNTIME_DIR") {
        Some(dir) => claim_marker(std::path::Path::new(&dir)),
        None => true,
    }
}

/// Atomically create-if-absent the marker under `runtime_dir`. Split out from
/// the env lookup so it is testable without mutating process-global env.
fn claim_marker(runtime_dir: &std::path::Path) -> bool {
    let marker = runtime_dir.join("wayle").join("lock-on-start.done");
    if let Some(parent) = marker.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    // create_new is the atomic first-writer-wins test: exactly one caller per
    // session sees Ok, so a race between two shells can't double-lock.
    match std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&marker)
    {
        Ok(_) => true,                                                   // first start → lock
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => false, // restart → skip
        Err(_) => true,                                                  // unknown error → fail secure
    }
}

#[cfg(test)]
mod tests {
    use super::claim_marker;

    #[test]
    fn claim_marker_locks_once_per_session() {
        // Fixed temp path (no rng available); clear any leftover first.
        let dir = std::env::temp_dir().join("wayle-lock-on-start-test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();

        assert!(claim_marker(&dir), "first start of a session locks");
        assert!(!claim_marker(&dir), "a restart within the session does not");
        assert!(!claim_marker(&dir), "and stays not-locking");

        let _ = std::fs::remove_dir_all(&dir);
    }
}
