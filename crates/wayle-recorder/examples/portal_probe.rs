//! Standalone ScreenCast portal probe.
//!
//! Mirrors `wayle_recorder::portal::open_screencast` exactly, but as a runnable
//! binary with `ashpd`/`zbus` tracing turned up, so we can see precisely which
//! D-Bus call fails (e.g. the "Invalid session" that breaks recording after the
//! picker). It does NOT touch GStreamer or claim any `com.wayle.*` D-Bus name,
//! so it can run alongside a live `wayle shell`.
//!
//! Run:
//!   RUST_LOG=ashpd=trace,zbus=debug,portal_probe=trace \
//!     cargo run -p wayle-recorder --example portal_probe
//!
//! Then pick a monitor in the picker. The log shows each request/response.

use std::{fs, path::PathBuf};

use ashpd::{
    desktop::{
        PersistMode,
        screencast::{CursorMode, Screencast, SelectSourcesOptions, SourceType},
    },
    enumflags2::BitFlags,
};
use tracing::{info, warn};

fn restore_token_path() -> Option<PathBuf> {
    let state_home = match std::env::var_os("XDG_STATE_HOME") {
        Some(v) if !v.is_empty() => PathBuf::from(v),
        _ => PathBuf::from(std::env::var_os("HOME")?).join(".local/state"),
    };
    Some(state_home.join("wayle").join("screencast.token"))
}

fn load_restore_token() -> Option<String> {
    let token = fs::read_to_string(restore_token_path()?).ok()?;
    let token = token.trim();
    (!token.is_empty()).then(|| token.to_owned())
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let show_cursor = std::env::args().any(|a| a == "--cursor");
    info!(show_cursor, "probe: starting screencast negotiation");

    info!("probe: Screencast::new()");
    let proxy = Screencast::new().await?;

    info!("probe: create_session()");
    let session = proxy.create_session(Default::default()).await?;
    info!("probe: session created");

    let cursor_mode = if show_cursor {
        CursorMode::Embedded
    } else {
        CursorMode::Hidden
    };
    let sources: BitFlags<SourceType> = SourceType::Monitor.into();
    let stored_token = load_restore_token();
    info!(
        has_token = stored_token.is_some(),
        "probe: select_sources()"
    );

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
        .await?
        .response()?;
    info!("probe: select_sources OK — calling start() (picker should appear now)");

    let streams = proxy
        .start(&session, None, Default::default())
        .await?
        .response()?;
    info!("probe: start() OK");

    if let Some(token) = streams.restore_token() {
        info!("probe: got restore token (len {})", token.len());
    }

    let stream = streams
        .streams()
        .first()
        .ok_or("screencast returned no streams")?;
    info!(node = stream.pipe_wire_node_id(), size = ?stream.size(), "probe: stream");

    info!("probe: open_pipe_wire_remote()");
    let fd = proxy
        .open_pipe_wire_remote(&session, Default::default())
        .await?;
    info!(?fd, "probe: SUCCESS — full negotiation completed");

    warn!("probe: leaving session open; exiting");
    Ok(())
}
