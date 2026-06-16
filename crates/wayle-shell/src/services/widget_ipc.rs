//! Unix-socket JSON-RPC server for runtime widget updates.
//!
//! Listens on the widget socket and forwards each `widget.update` request onto
//! an in-process [`WidgetBus`]. Bar widgets subscribe to the bus and apply
//! updates addressed to their own config id, so an external client can drive a
//! widget exactly as its own command output would.

use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    net::{UnixListener, UnixStream},
    sync::broadcast,
};
use tracing::{info, warn};
use wayle_ipc::widget_socket::{
    METHOD_TOAST_SHOW, METHOD_WIDGET_UPDATE, Request, Response, ToastShowParams,
    WidgetUpdateParams, socket_path,
};

/// Broadcast backlog before lagging receivers drop the oldest updates.
const CHANNEL_CAPACITY: usize = 64;

/// A runtime update destined for the widget whose config id matches `id`.
#[derive(Debug, Clone)]
pub struct WidgetUpdate {
    /// Target widget config id.
    pub id: String,
    /// Output payload, interpreted like the widget's command output.
    pub output: String,
}

/// In-process broadcast bus carrying widget updates from the socket to widgets.
#[derive(Clone)]
pub struct WidgetBus {
    tx: broadcast::Sender<WidgetUpdate>,
}

impl WidgetBus {
    /// Creates an empty bus.
    #[must_use]
    pub fn new() -> Self {
        let (tx, _) = broadcast::channel(CHANNEL_CAPACITY);
        Self { tx }
    }

    /// Subscribes a new receiver. Widgets call this to observe updates.
    #[must_use]
    pub fn subscribe(&self) -> broadcast::Receiver<WidgetUpdate> {
        self.tx.subscribe()
    }

    /// Publishes an update to all subscribers (no-op when none are listening).
    fn publish(&self, update: WidgetUpdate) {
        let _ = self.tx.send(update);
    }
}

impl Default for WidgetBus {
    fn default() -> Self {
        Self::new()
    }
}

/// A custom on-screen toast request pushed over the socket.
#[derive(Debug, Clone)]
pub struct ToastRequest {
    /// Toast text.
    pub label: String,
    /// Optional icon name.
    pub icon: Option<String>,
    /// Optional progress percentage (0-100); shows a progress bar when set.
    pub percentage: Option<f64>,
    /// Optional auto-dismiss duration in milliseconds (OSD default when `None`).
    pub duration_ms: Option<u32>,
}

/// In-process broadcast bus carrying toast requests from the socket to the OSD.
#[derive(Clone)]
pub struct ToastBus {
    tx: broadcast::Sender<ToastRequest>,
}

impl ToastBus {
    /// Creates an empty bus.
    #[must_use]
    pub fn new() -> Self {
        let (tx, _) = broadcast::channel(CHANNEL_CAPACITY);
        Self { tx }
    }

    /// Subscribes a new receiver. The OSD calls this to observe toast requests.
    #[must_use]
    pub fn subscribe(&self) -> broadcast::Receiver<ToastRequest> {
        self.tx.subscribe()
    }

    /// Publishes a toast (no-op when no subscribers are listening).
    fn publish(&self, toast: ToastRequest) {
        let _ = self.tx.send(toast);
    }
}

impl Default for ToastBus {
    fn default() -> Self {
        Self::new()
    }
}

/// The set of in-process buses the socket dispatches requests onto.
#[derive(Clone)]
struct Buses {
    widget: WidgetBus,
    toast: ToastBus,
}

/// Errors raised when starting the widget socket listener.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The socket could not be bound.
    #[error("failed to bind widget socket at {path}: {source}")]
    Bind {
        /// Socket path.
        path: String,
        /// Underlying I/O error.
        source: std::io::Error,
    },
}

/// Starts the widget socket listener, forwarding requests onto `bus`.
///
/// The listener runs on a detached task; a stale socket file is removed first
/// and the parent directory is created if needed.
///
/// # Errors
///
/// Returns [`Error::Bind`] when the socket cannot be bound.
pub async fn start(widget_bus: WidgetBus, toast_bus: ToastBus) -> Result<(), Error> {
    let path = socket_path();

    if let Some(parent) = path.parent() {
        let _ = tokio::fs::create_dir_all(parent).await;
    }
    // A leftover socket from a previous run would make bind fail with EADDRINUSE.
    let _ = tokio::fs::remove_file(&path).await;

    let listener = UnixListener::bind(&path).map_err(|source| Error::Bind {
        path: path.display().to_string(),
        source,
    })?;

    // Restrict the socket to the owner. It lives under the user-private
    // $XDG_RUNTIME_DIR, but tightening to 0600 makes the intent explicit and
    // guards setups that fall back to a world-readable /tmp path.
    {
        use std::os::unix::fs::PermissionsExt;
        if let Err(err) = std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600)) {
            warn!(error = %err, "could not restrict widget socket permissions");
        }
    }

    info!("Widget socket listening at {}", path.display());

    let buses = Buses {
        widget: widget_bus,
        toast: toast_bus,
    };

    tokio::spawn(async move {
        loop {
            match listener.accept().await {
                Ok((stream, _addr)) => {
                    tokio::spawn(handle_connection(stream, buses.clone()));
                }
                Err(err) => warn!(error = %err, "widget socket accept failed"),
            }
        }
    });

    Ok(())
}

/// Serves newline-delimited JSON-RPC requests on a single connection.
async fn handle_connection(stream: UnixStream, buses: Buses) {
    let (read_half, mut write_half) = stream.into_split();
    let mut reader = BufReader::new(read_half);
    let mut line = String::new();

    loop {
        line.clear();
        match reader.read_line(&mut line).await {
            Ok(0) => break,
            Ok(_) => {
                let response = process_request(line.trim(), &buses);
                let Ok(mut encoded) = serde_json::to_string(&response) else {
                    break;
                };
                encoded.push('\n');
                if write_half.write_all(encoded.as_bytes()).await.is_err() {
                    break;
                }
            }
            Err(err) => {
                warn!(error = %err, "widget socket read failed");
                break;
            }
        }
    }
}

/// Parses and dispatches a single JSON-RPC request line.
fn process_request(line: &str, buses: &Buses) -> Response {
    let request: Request = match serde_json::from_str(line) {
        Ok(request) => request,
        Err(err) => return Response::err(0, -32700, format!("parse error: {err}")),
    };

    match request.method.as_str() {
        METHOD_WIDGET_UPDATE => {
            match serde_json::from_value::<WidgetUpdateParams>(request.params) {
                Ok(params) => {
                    buses.widget.publish(WidgetUpdate {
                        id: params.id,
                        output: params.output,
                    });
                    Response::ok(request.id)
                }
                Err(err) => Response::err(request.id, -32602, format!("invalid params: {err}")),
            }
        }
        METHOD_TOAST_SHOW => match serde_json::from_value::<ToastShowParams>(request.params) {
            Ok(params) => {
                buses.toast.publish(ToastRequest {
                    label: params.label,
                    icon: params.icon,
                    percentage: params.percentage,
                    duration_ms: params.duration_ms,
                });
                Response::ok(request.id)
            }
            Err(err) => Response::err(request.id, -32602, format!("invalid params: {err}")),
        },
        other => Response::err(request.id, -32601, format!("unknown method: {other}")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_buses() -> Buses {
        Buses {
            widget: WidgetBus::new(),
            toast: ToastBus::new(),
        }
    }

    #[test]
    fn valid_update_publishes_and_acks() {
        let buses = test_buses();
        let mut rx = buses.widget.subscribe();

        let line = r#"{"jsonrpc":"2.0","method":"widget.update","params":{"id":"gpu","output":"42"},"id":7}"#;
        let response = process_request(line, &buses);

        assert!(response.error.is_none());
        assert_eq!(response.id, 7);
        let update = rx.try_recv().expect("update published");
        assert_eq!(update.id, "gpu");
        assert_eq!(update.output, "42");
    }

    #[test]
    fn valid_toast_publishes_and_acks() {
        let buses = test_buses();
        let mut rx = buses.toast.subscribe();

        let line = r#"{"jsonrpc":"2.0","method":"toast.show","params":{"label":"hi","icon":"ld-bell-symbolic"},"id":9}"#;
        let response = process_request(line, &buses);

        assert!(response.error.is_none());
        assert_eq!(response.id, 9);
        let toast = rx.try_recv().expect("toast published");
        assert_eq!(toast.label, "hi");
        assert_eq!(toast.icon.as_deref(), Some("ld-bell-symbolic"));
        assert_eq!(toast.duration_ms, None);
    }

    #[test]
    fn unknown_method_errors() {
        let buses = test_buses();
        let line = r#"{"jsonrpc":"2.0","method":"widget.nope","params":{},"id":1}"#;
        let response = process_request(line, &buses);
        assert!(response.error.is_some());
    }

    #[test]
    fn invalid_params_error() {
        let buses = test_buses();
        let line = r#"{"jsonrpc":"2.0","method":"widget.update","params":{"id":"gpu"},"id":1}"#;
        let response = process_request(line, &buses);
        assert!(response.error.is_some());
    }

    #[test]
    fn malformed_json_errors() {
        let buses = test_buses();
        let response = process_request("not json", &buses);
        assert!(response.error.is_some());
    }

    #[tokio::test]
    async fn socket_round_trip_publishes_and_responds() {
        let dir = std::env::temp_dir().join(format!("wayle-widget-test-{}", std::process::id()));
        tokio::fs::create_dir_all(&dir).await.unwrap();
        let path = dir.join("widget.sock");
        let _ = tokio::fs::remove_file(&path).await;

        let listener = UnixListener::bind(&path).unwrap();
        let buses = test_buses();
        let mut rx = buses.widget.subscribe();
        let serve_buses = buses.clone();
        tokio::spawn(async move {
            if let Ok((stream, _)) = listener.accept().await {
                handle_connection(stream, serve_buses).await;
            }
        });

        let stream = UnixStream::connect(&path).await.unwrap();
        let (read_half, mut write_half) = stream.into_split();
        let mut line = serde_json::to_string(&Request::widget_update("gpu", "42")).unwrap();
        line.push('\n');
        write_half.write_all(line.as_bytes()).await.unwrap();

        let mut reader = BufReader::new(read_half);
        let mut response_line = String::new();
        reader.read_line(&mut response_line).await.unwrap();
        let response: Response = serde_json::from_str(response_line.trim()).unwrap();
        assert!(response.error.is_none());

        let update = rx.recv().await.unwrap();
        assert_eq!(update.id, "gpu");
        assert_eq!(update.output, "42");

        let _ = tokio::fs::remove_dir_all(&dir).await;
    }
}
