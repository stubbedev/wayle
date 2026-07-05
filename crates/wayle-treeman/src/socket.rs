//! treeman daemon socket: path resolution and the event subscription.
//!
//! Wire format is newline-delimited JSON over a unix domain socket (protocol
//! v2). We only use the streaming `event_subscribe` method as a change signal —
//! the actual bucketed status comes from `treeman status --format json`, which
//! reads the store directly and keeps working while the daemon restarts.

use std::{env, path::PathBuf};

use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader, Lines},
    net::UnixStream,
};

use crate::error::{Error, Result};

/// Line reader over the subscribed event stream; each line is one event.
pub(crate) type EventStream = Lines<BufReader<UnixStream>>;

/// The subscribe request. An empty filter matches every future event; that is
/// intentional — any daemon activity is a hint to re-read status, and the
/// re-read is debounced so bursts collapse to one refresh.
const SUBSCRIBE_REQUEST: &str = r#"{"method":"event_subscribe","event_subscribe":{}}"#;

/// Resolves the daemon socket path, mirroring treeman's own lookup order:
/// `$TREEMAN_SOCKET` → `$XDG_RUNTIME_DIR/treeman.sock` →
/// `$XDG_DATA_HOME/treeman/treeman.sock` → `~/.local/share/treeman/treeman.sock`.
pub(crate) fn socket_path() -> Result<PathBuf> {
    if let Some(p) = env::var_os("TREEMAN_SOCKET").filter(|p| !p.is_empty()) {
        return Ok(PathBuf::from(p));
    }
    if let Some(rt) = env::var_os("XDG_RUNTIME_DIR").filter(|p| !p.is_empty()) {
        return Ok(PathBuf::from(rt).join("treeman.sock"));
    }
    let data_home = env::var_os("XDG_DATA_HOME")
        .filter(|p| !p.is_empty())
        .map(PathBuf::from)
        .or_else(|| env::var_os("HOME").map(|h| PathBuf::from(h).join(".local/share")))
        .ok_or(Error::NoSocketPath)?;
    Ok(data_home.join("treeman/treeman.sock"))
}

/// Connects the socket and opens an event subscription.
///
/// Returns a line reader that yields one JSON event per line. Historical events
/// are not replayed — the caller fetches a full status snapshot on connect.
pub(crate) async fn connect_subscribe() -> Result<EventStream> {
    let path = socket_path()?;
    let stream = UnixStream::connect(&path).await?;
    let mut reader = BufReader::new(stream);
    reader
        .get_mut()
        .write_all(format!("{SUBSCRIBE_REQUEST}\n").as_bytes())
        .await?;
    Ok(reader.lines())
}
