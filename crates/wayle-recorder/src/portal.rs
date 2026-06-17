//! xdg-desktop-portal ScreenCast negotiation via [`ashpd`].
//!
//! Returns a PipeWire remote file descriptor + node id that `pipewiresrc` can
//! consume, which is the Wayland-correct way to capture the screen.

use std::{fs, os::fd::OwnedFd, path::PathBuf};

use ashpd::{
    desktop::{
        PersistMode,
        screencast::{CursorMode, Screencast, SelectSourcesOptions, SourceType},
    },
    enumflags2::BitFlags,
};

use crate::Error;

/// Path to the cached ScreenCast restore token:
/// `$XDG_STATE_HOME/wayle/screencast.token` (or `~/.local/state/...`).
///
/// Returns `None` if neither `XDG_STATE_HOME` nor `HOME` is set.
fn restore_token_path() -> Option<PathBuf> {
    let state_home = match std::env::var_os("XDG_STATE_HOME") {
        Some(v) if !v.is_empty() => PathBuf::from(v),
        _ => PathBuf::from(std::env::var_os("HOME")?).join(".local/state"),
    };
    Some(state_home.join("wayle").join("screencast.token"))
}

/// Reads the cached restore token, if one was saved by a prior session.
fn load_restore_token() -> Option<String> {
    let token = fs::read_to_string(restore_token_path()?).ok()?;
    let token = token.trim();
    (!token.is_empty()).then(|| token.to_owned())
}

/// Persists the restore token so the next session can skip the picker.
/// Best-effort: failures are ignored (a missing token just re-prompts).
fn save_restore_token(token: &str) {
    let Some(path) = restore_token_path() else {
        return;
    };
    if let Some(dir) = path.parent() {
        let _ = fs::create_dir_all(dir);
    }
    let _ = fs::write(path, token);
}

/// A negotiated screen-capture stream.
pub(crate) struct ScreenCast {
    /// PipeWire remote file descriptor. Kept alive for the pipeline's lifetime.
    pub(crate) fd: OwnedFd,
    /// PipeWire node id of the captured monitor.
    pub(crate) node_id: u32,
    /// Captured stream size in pixels, when the portal reports it.
    pub(crate) size: Option<(i32, i32)>,
}

/// Requests a monitor ScreenCast session from the portal.
///
/// # Errors
///
/// Returns [`Error::Portal`] if the portal is unavailable, the user cancels the
/// picker, or no stream is returned.
pub(crate) async fn open_screencast(show_cursor: bool) -> Result<ScreenCast, Error> {
    let proxy = Screencast::new().await.map_err(|e| portal_err(&e))?;
    let session = proxy
        .create_session(Default::default())
        .await
        .map_err(|e| portal_err(&e))?;

    let cursor_mode = if show_cursor {
        CursorMode::Embedded
    } else {
        CursorMode::Hidden
    };
    let sources: BitFlags<SourceType> = SourceType::Monitor.into();

    // Reuse the previous grant: Persistent + a cached restore token make the
    // portal replay the prior monitor selection without showing the picker.
    // The picker appears only on the first ever capture, or after the token is
    // invalidated (portal/compositor restart, version bump, revoked grant).
    let stored_token = load_restore_token();

    proxy
        .select_sources(
            &session,
            SelectSourcesOptions::default()
                .set_cursor_mode(cursor_mode)
                .set_sources(sources)
                .set_multiple(false)
                .set_persist_mode(PersistMode::ExplicitlyRevoked)
                .set_restore_token(stored_token.as_deref()),
        )
        .await
        .map_err(|e| portal_err(&e))?;

    let streams = proxy
        .start(&session, None, Default::default())
        .await
        .map_err(|e| portal_err(&e))?
        .response()
        .map_err(|e| portal_err(&e))?;

    // Persist the (possibly refreshed) token so the next session is promptless.
    if let Some(token) = streams.restore_token() {
        save_restore_token(token);
    }

    let stream = streams
        .streams()
        .first()
        .ok_or_else(|| Error::Portal(String::from("screencast returned no streams")))?;

    let node_id = stream.pipe_wire_node_id();
    let size = stream.size();

    let fd = proxy
        .open_pipe_wire_remote(&session, Default::default())
        .await
        .map_err(|e| portal_err(&e))?;

    Ok(ScreenCast { fd, node_id, size })
}

fn portal_err(error: &ashpd::Error) -> Error {
    Error::Portal(error.to_string())
}
