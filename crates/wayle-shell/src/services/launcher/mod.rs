//! Launcher session socket server.
//!
//! Listens on the launcher socket (see `wayle_ipc::launcher_socket`) and
//! bridges each CLI session to the GTK-thread [`Launcher`] component through
//! a process-global Relm4 sender. The connection's lifetime is the session's
//! lifetime: client EOF cancels the surface, and the component's terminal
//! frame (result/cancelled/busy) closes the connection.
//!
//! [`Launcher`]: crate::shell::launcher::Launcher

use std::sync::{
    OnceLock,
    atomic::{AtomicU64, Ordering},
};

use relm4::Sender;
use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    net::{UnixListener, UnixStream},
    sync::{mpsc, oneshot},
};
use tracing::{info, warn};
use wayle_ipc::launcher_socket::{ClientFrame, ServerFrame, socket_path};

use crate::shell::launcher::LauncherInput;

/// GTK-thread sender into the launcher component.
static SURFACE_SENDER: OnceLock<Sender<LauncherInput>> = OnceLock::new();

/// Monotonic session/connection ids.
static SESSION_IDS: AtomicU64 = AtomicU64::new(1);

/// Records the launcher component's input sender. Called once during shell
/// init; later calls are ignored.
pub(crate) fn register_sender(sender: Sender<LauncherInput>) {
    if SURFACE_SENDER.set(sender).is_err() {
        warn!("launcher sender already registered");
    }
}

/// Errors raised when starting the launcher socket listener.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The socket could not be bound.
    #[error("failed to bind launcher socket at {path}: {source}")]
    Bind {
        /// Socket path.
        path: String,
        /// Underlying I/O error.
        source: std::io::Error,
    },
}

/// Starts the launcher socket listener on a detached task.
///
/// # Errors
///
/// Returns [`Error::Bind`] when the socket cannot be bound.
pub async fn start() -> Result<(), Error> {
    let path = socket_path();
    if let Some(parent) = path.parent() {
        let _ = tokio::fs::create_dir_all(parent).await;
    }
    let _ = tokio::fs::remove_file(&path).await;

    let listener = UnixListener::bind(&path).map_err(|source| Error::Bind {
        path: path.display().to_string(),
        source,
    })?;
    {
        use std::os::unix::fs::PermissionsExt;
        if let Err(err) = std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600)) {
            warn!(error = %err, "could not restrict launcher socket permissions");
        }
    }
    info!("Launcher socket listening at {}", path.display());

    tokio::spawn(async move {
        loop {
            match listener.accept().await {
                Ok((stream, _addr)) => {
                    tokio::spawn(handle_connection(stream));
                }
                Err(err) => warn!(error = %err, "launcher socket accept failed"),
            }
        }
    });
    Ok(())
}

/// Reads the mandatory first `open` frame.
async fn read_open_frame(
    reader: &mut BufReader<tokio::net::unix::OwnedReadHalf>,
) -> Option<(Box<wayle_ipc::launcher_socket::SessionOptions>, bool)> {
    let mut line = String::new();
    match reader.read_line(&mut line).await {
        Ok(0) | Err(_) => return None,
        Ok(_) => {}
    }
    match serde_json::from_str(line.trim()) {
        Ok(ClientFrame::Open { options, replace }) => Some((options, replace)),
        Ok(_) => {
            warn!("launcher socket: first frame was not open");
            None
        }
        Err(err) => {
            warn!(error = %err, "launcher socket: malformed open frame");
            None
        }
    }
}

/// Serves one CLI session connection.
async fn handle_connection(stream: UnixStream) {
    let (read_half, mut write_half) = stream.into_split();
    let mut reader = BufReader::new(read_half);

    let Some((options, replace)) = read_open_frame(&mut reader).await else {
        return;
    };

    let Some(sender) = SURFACE_SENDER.get() else {
        warn!("launcher socket: surface not ready");
        let _ = write_frame(&mut write_half, &ServerFrame::Cancelled { code: 1 }).await;
        return;
    };

    let id = SESSION_IDS.fetch_add(1, Ordering::Relaxed);
    let (rows_tx, rows_rx) = mpsc::channel::<Vec<String>>(16);
    let (reply_tx, reply_rx) = oneshot::channel::<ServerFrame>();
    sender.emit(LauncherInput::OpenSession {
        id,
        options,
        replace,
        reply: reply_tx,
        rows: rows_rx,
    });
    let _ = write_frame(&mut write_half, &ServerFrame::Opened).await;

    pump_session(id, sender, reader, write_half, rows_tx, reply_rx).await;
}

/// Pumps client frames and waits for the component's terminal frame.
async fn pump_session(
    id: u64,
    sender: &Sender<LauncherInput>,
    mut reader: BufReader<tokio::net::unix::OwnedReadHalf>,
    mut write_half: tokio::net::unix::OwnedWriteHalf,
    rows_tx: mpsc::Sender<Vec<String>>,
    mut reply_rx: oneshot::Receiver<ServerFrame>,
) {
    // Option so RowsDone can drop the sender and close the row stream.
    let mut rows_tx = Some(rows_tx);
    let mut line = String::new();
    loop {
        line.clear();
        tokio::select! {
            reply = &mut reply_rx => {
                if let Ok(frame) = reply {
                    let _ = write_frame(&mut write_half, &frame).await;
                }
                return;
            }
            read = reader.read_line(&mut line) => {
                match read {
                    Ok(0) | Err(_) => {
                        // Client died (ctrl-C): tear the surface down, then
                        // drain the reply so the component isn't left waiting.
                        sender.emit(LauncherInput::ClientGone { id });
                        let _ = reply_rx.await;
                        return;
                    }
                    Ok(_) => on_client_frame(&line, &mut rows_tx).await,
                }
            }
        }
    }
}

/// Applies one non-terminal client frame.
async fn on_client_frame(line: &str, rows_tx: &mut Option<mpsc::Sender<Vec<String>>>) {
    match serde_json::from_str::<ClientFrame>(line.trim()) {
        Ok(ClientFrame::Rows { items }) => {
            if let Some(tx) = rows_tx
                && tx.send(items).await.is_err()
            {
                *rows_tx = None;
            }
        }
        Ok(ClientFrame::RowsDone) => {
            // Dropping the sender closes the row stream.
            *rows_tx = None;
        }
        Ok(ClientFrame::Open { .. }) => {
            warn!("launcher socket: duplicate open ignored");
        }
        Err(err) => {
            warn!(error = %err, "launcher socket: malformed frame");
        }
    }
}

async fn write_frame(
    write_half: &mut tokio::net::unix::OwnedWriteHalf,
    frame: &ServerFrame,
) -> std::io::Result<()> {
    let Ok(mut encoded) = serde_json::to_string(frame) else {
        return Ok(());
    };
    encoded.push('\n');
    write_half.write_all(encoded.as_bytes()).await?;
    write_half.flush().await
}
