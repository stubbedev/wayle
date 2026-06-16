//! xdg-desktop-portal ScreenCast negotiation via [`ashpd`].
//!
//! Returns a PipeWire remote file descriptor + node id that `pipewiresrc` can
//! consume, which is the Wayland-correct way to capture the screen.

use std::os::fd::OwnedFd;

use ashpd::{
    desktop::{
        PersistMode,
        screencast::{CursorMode, Screencast, SelectSourcesOptions, SourceType},
    },
    enumflags2::BitFlags,
};

use crate::Error;

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

    proxy
        .select_sources(
            &session,
            SelectSourcesOptions::default()
                .set_cursor_mode(cursor_mode)
                .set_sources(sources)
                .set_multiple(false)
                .set_persist_mode(PersistMode::DoNot),
        )
        .await
        .map_err(|e| portal_err(&e))?;

    let streams = proxy
        .start(&session, None, Default::default())
        .await
        .map_err(|e| portal_err(&e))?
        .response()
        .map_err(|e| portal_err(&e))?;

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
