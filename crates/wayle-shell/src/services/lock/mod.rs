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
