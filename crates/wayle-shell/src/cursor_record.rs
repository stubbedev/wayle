//! Records the running session's cursor so the greeter can match it next boot.
//!
//! The greeter runs pre-login and cannot ask the (not-yet-started) session what
//! cursor the user actually sees, so it best-effort reads dotfiles — which miss
//! cursors set only via the compositor's exported `XCURSOR_*` environment or
//! Hyprland's `hyprcursor`. Here, from inside the live session, those env vars
//! ARE the resolved truth. We stash them in the user's XDG state dir; the
//! greeter reads that file first (see `wayle-greeter`'s `cursor` module), so the
//! login screen keeps exactly the theme/size of the last session used.

use std::fs;

use tracing::debug;
use wayle_core::paths::ConfigPaths;

/// State-dir-relative path the greeter reads. Kept in sync with
/// `wayle-greeter`'s `cursor::RECORDED_REL`.
const FILE: &str = "greeter-cursor";

/// Writes the live `XCURSOR_THEME`/`XCURSOR_SIZE` (size falling back to
/// `HYPRCURSOR_SIZE`) to `$XDG_STATE_HOME/wayle/greeter-cursor`. Best-effort:
/// any failure is logged and ignored, and nothing is written when neither a
/// theme nor a size is exported (so a good record is never clobbered by blanks).
pub(crate) fn record() {
    let theme = std::env::var("XCURSOR_THEME")
        .ok()
        .filter(|t| !t.is_empty());
    let size = std::env::var("XCURSOR_SIZE")
        .ok()
        .or_else(|| std::env::var("HYPRCURSOR_SIZE").ok())
        .and_then(|v| v.trim().parse::<u32>().ok());

    if theme.is_none() && size.is_none() {
        return;
    }

    let Ok(dir) = ConfigPaths::state_dir() else {
        return;
    };
    let mut body = String::new();
    if let Some(theme) = theme {
        body.push_str(&format!("theme={theme}\n"));
    }
    if let Some(size) = size {
        body.push_str(&format!("size={size}\n"));
    }
    if let Err(err) = fs::write(dir.join(FILE), body) {
        debug!(%err, "could not record cursor for greeter");
    }
}
