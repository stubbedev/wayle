//! Remembers Do Not Disturb across restarts.
//!
//! DND is a sticky user decision — "stop interrupting me" — but it lived only
//! in the service's [`Property`](wayle_core::Property), so restarting the shell
//! (a config switch, an upgrade) silently let notifications back through. The
//! flag is mirrored into the XDG state dir and read back when the service
//! starts.

use std::fs;

use tracing::debug;
use wayle_core::paths::ConfigPaths;

const FILE: &str = "dnd";

pub(crate) fn load() -> bool {
    let Ok(dir) = ConfigPaths::state_dir() else {
        return false;
    };
    fs::read_to_string(dir.join(FILE))
        .map(|body| body.trim() == "on")
        .unwrap_or(false)
}

pub(crate) fn save(enabled: bool) {
    let Ok(dir) = ConfigPaths::state_dir() else {
        return;
    };
    let body = if enabled { "on\n" } else { "off\n" };
    if let Err(err) = fs::write(dir.join(FILE), body) {
        debug!(%err, "could not record do-not-disturb state");
    }
}
