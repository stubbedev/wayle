//! Error types for the treeman status service.

use std::io;

/// Errors from fetching or subscribing to treeman status.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The `treeman` binary could not be spawned or exited non-zero.
    #[error("treeman command failed: {0}")]
    Command(String),

    /// The daemon socket could not be located (no `$XDG_RUNTIME_DIR` etc.).
    #[error("treeman socket path could not be resolved")]
    NoSocketPath,

    /// I/O failure talking to the daemon socket.
    #[error("treeman socket I/O error")]
    Io(#[from] io::Error),

    /// The status JSON could not be parsed.
    #[error("treeman status JSON parse error")]
    Parse(#[from] serde_json::Error),
}

/// Convenience result alias.
pub type Result<T> = std::result::Result<T, Error>;
