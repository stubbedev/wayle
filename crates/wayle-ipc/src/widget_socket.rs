//! Unix-socket JSON-RPC protocol for pushing runtime updates to bar widgets.
//!
//! The running shell listens on a unix socket and accepts newline-delimited
//! JSON-RPC 2.0 requests. External clients (the `wayle widget` CLI, or any
//! script) push an output payload to a widget addressed by its config id; the
//! payload is delivered to the matching widget exactly as if its own command
//! had produced that output.

use std::path::PathBuf;

use serde::{Deserialize, Serialize};
use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    net::UnixStream,
};

/// JSON-RPC method that updates a widget's output by id.
pub const METHOD_WIDGET_UPDATE: &str = "widget.update";

/// JSON-RPC method that shows a custom on-screen toast.
pub const METHOD_TOAST_SHOW: &str = "toast.show";

/// Resolves the widget socket path: `$XDG_RUNTIME_DIR/wayle/widget.sock`,
/// falling back to `/tmp/wayle-widget.sock` when the runtime dir is unset.
#[must_use]
pub fn socket_path() -> PathBuf {
    match std::env::var_os("XDG_RUNTIME_DIR") {
        Some(dir) => PathBuf::from(dir).join("wayle").join("widget.sock"),
        None => PathBuf::from("/tmp/wayle-widget.sock"),
    }
}

/// Parameters for [`METHOD_WIDGET_UPDATE`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WidgetUpdateParams {
    /// Config id of the target widget (e.g. the `id` of a custom module).
    pub id: String,
    /// Output payload, interpreted exactly like the widget's command output
    /// (plain text, or JSON with `text`/`alt`/`percentage`/`class`/`tooltip`).
    pub output: String,
}

/// Parameters for [`METHOD_TOAST_SHOW`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToastShowParams {
    /// Toast text. Optional when `preset` supplies one; an explicit label
    /// overrides the preset's label.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub label: Option<String>,
    /// Optional icon name shown beside the text.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub icon: Option<String>,
    /// Optional progress percentage (0-100). When set, the toast shows a
    /// progress bar like the volume/brightness OSD; otherwise it renders as a
    /// plain icon + label toast.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub percentage: Option<f64>,
    /// Optional auto-dismiss duration in milliseconds; falls back to the toast
    /// config duration when unset.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub duration_ms: Option<u32>,
    /// Optional preset id (`[[toasts.presets]]`) to base this toast on.
    /// Explicit fields above override the preset's values.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub preset: Option<String>,
    /// Optional extra CSS class applied to the toast for custom styling.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub class: Option<String>,
}

/// A JSON-RPC 2.0 request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Request {
    /// Protocol marker; always `"2.0"`.
    pub jsonrpc: String,
    /// Invoked method name.
    pub method: String,
    /// Method parameters.
    pub params: serde_json::Value,
    /// Correlation id (echoed in the response).
    pub id: u64,
}

impl Request {
    /// Builds a [`METHOD_WIDGET_UPDATE`] request.
    #[must_use]
    pub fn widget_update(id: &str, output: &str) -> Self {
        let params = serde_json::to_value(WidgetUpdateParams {
            id: id.to_owned(),
            output: output.to_owned(),
        })
        .unwrap_or(serde_json::Value::Null);

        Self {
            jsonrpc: String::from("2.0"),
            method: String::from(METHOD_WIDGET_UPDATE),
            params,
            id: 1,
        }
    }

    /// Builds a [`METHOD_TOAST_SHOW`] request.
    #[must_use]
    pub fn toast_show(
        label: Option<&str>,
        icon: Option<&str>,
        percentage: Option<f64>,
        duration_ms: Option<u32>,
        preset: Option<&str>,
        class: Option<&str>,
    ) -> Self {
        let params = serde_json::to_value(ToastShowParams {
            label: label.map(str::to_owned),
            icon: icon.map(str::to_owned),
            percentage,
            duration_ms,
            preset: preset.map(str::to_owned),
            class: class.map(str::to_owned),
        })
        .unwrap_or(serde_json::Value::Null);

        Self {
            jsonrpc: String::from("2.0"),
            method: String::from(METHOD_TOAST_SHOW),
            params,
            id: 1,
        }
    }
}

/// A JSON-RPC 2.0 response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Response {
    /// Protocol marker; always `"2.0"`.
    pub jsonrpc: String,
    /// Result on success.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<serde_json::Value>,
    /// Error on failure.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<RpcError>,
    /// Correlation id from the request.
    pub id: u64,
}

impl Response {
    /// Builds a success response.
    #[must_use]
    pub fn ok(id: u64) -> Self {
        Self {
            jsonrpc: String::from("2.0"),
            result: Some(serde_json::Value::String(String::from("ok"))),
            error: None,
            id,
        }
    }

    /// Builds an error response.
    #[must_use]
    pub fn err(id: u64, code: i64, message: impl Into<String>) -> Self {
        Self {
            jsonrpc: String::from("2.0"),
            result: None,
            error: Some(RpcError {
                code,
                message: message.into(),
            }),
            id,
        }
    }
}

/// A JSON-RPC 2.0 error object.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RpcError {
    /// Numeric error code.
    pub code: i64,
    /// Human-readable error message.
    pub message: String,
}

/// Errors raised by the widget socket client.
#[derive(Debug, thiserror::Error)]
pub enum ClientError {
    /// The shell is not running or the socket is unavailable.
    #[error("cannot connect to wayle widget socket at {path}: {source}")]
    Connect {
        /// Attempted socket path.
        path: String,
        /// Underlying I/O error.
        source: std::io::Error,
    },

    /// I/O failure while talking to the socket.
    #[error("widget socket I/O failed: {0}")]
    Io(#[from] std::io::Error),

    /// Serialization or deserialization failure.
    #[error("widget socket protocol error: {0}")]
    Protocol(#[from] serde_json::Error),

    /// The server returned a JSON-RPC error.
    #[error("widget update rejected: {0}")]
    Rejected(String),
}

/// Sends a single widget-update request and awaits the response.
///
/// # Errors
///
/// Returns [`ClientError`] when the socket is unreachable, the I/O fails, the
/// payload cannot be (de)serialized, or the server rejects the request.
pub async fn send_widget_update(id: &str, output: &str) -> Result<(), ClientError> {
    send_request(Request::widget_update(id, output)).await
}

/// Shows a custom on-screen toast and awaits the response.
///
/// # Errors
///
/// Returns [`ClientError`] when the socket is unreachable, the I/O fails, the
/// payload cannot be (de)serialized, or the server rejects the request.
pub async fn send_toast(
    label: Option<&str>,
    icon: Option<&str>,
    percentage: Option<f64>,
    duration_ms: Option<u32>,
    preset: Option<&str>,
    class: Option<&str>,
) -> Result<(), ClientError> {
    send_request(Request::toast_show(
        label,
        icon,
        percentage,
        duration_ms,
        preset,
        class,
    ))
    .await
}

/// Sends a single request over the widget socket and awaits the response.
async fn send_request(request: Request) -> Result<(), ClientError> {
    let path = socket_path();
    let stream = UnixStream::connect(&path)
        .await
        .map_err(|source| ClientError::Connect {
            path: path.display().to_string(),
            source,
        })?;

    let mut line = serde_json::to_string(&request)?;
    line.push('\n');

    let (read_half, mut write_half) = stream.into_split();
    write_half.write_all(line.as_bytes()).await?;
    write_half.flush().await?;

    let mut reader = BufReader::new(read_half);
    let mut response_line = String::new();
    reader.read_line(&mut response_line).await?;

    let response: Response = serde_json::from_str(response_line.trim())?;
    if let Some(error) = response.error {
        return Err(ClientError::Rejected(error.message));
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn widget_update_request_round_trips() {
        let request = Request::widget_update("gpu", "{\"text\":\"42\"}");
        let encoded = serde_json::to_string(&request).unwrap();
        let decoded: Request = serde_json::from_str(&encoded).unwrap();
        assert_eq!(decoded.method, METHOD_WIDGET_UPDATE);

        let params: WidgetUpdateParams = serde_json::from_value(decoded.params).unwrap();
        assert_eq!(params.id, "gpu");
        assert_eq!(params.output, "{\"text\":\"42\"}");
    }

    #[test]
    fn ok_response_has_no_error_field() {
        let encoded = serde_json::to_string(&Response::ok(3)).unwrap();
        assert!(!encoded.contains("error"));
        assert!(encoded.contains("\"result\""));
    }

    #[test]
    fn err_response_has_no_result_field() {
        let encoded = serde_json::to_string(&Response::err(3, -32601, "nope")).unwrap();
        assert!(!encoded.contains("result"));
        assert!(encoded.contains("\"error\""));
    }
}
