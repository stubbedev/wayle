//! Error types for the sway service.

use std::{
    fmt::{self, Display, Formatter},
    io,
};

/// Which socket role a connection error refers to.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SocketKind {
    /// The persistent socket used for request/reply command traffic.
    Command,
    /// The subscribed socket used for the event stream.
    EventStream,
}

impl Display for SocketKind {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> fmt::Result {
        match self {
            Self::Command => formatter.write_str("command"),
            Self::EventStream => formatter.write_str("event-stream"),
        }
    }
}

/// Errors produced by the sway service.
#[derive(thiserror::Error, Debug)]
pub enum Error {
    /// `SWAYSOCK` is unset, so sway is not reachable.
    #[error("sway is not running or SWAYSOCK is not set")]
    SwayNotRunning,

    /// Connecting the named socket failed.
    #[error("cannot connect to sway {kind} socket")]
    IpcConnectionFailed {
        /// Which socket role the connection attempt was for.
        kind: SocketKind,
        /// Underlying I/O error.
        #[source]
        source: io::Error,
    },

    /// Reading or writing the socket failed.
    #[error("sway socket I/O error")]
    Io(#[from] io::Error),

    /// The IPC reply carried an invalid magic header.
    #[error("sway sent an invalid IPC magic header")]
    InvalidMagic,

    /// A JSON message could not be serialized or parsed.
    #[error("cannot parse sway JSON message")]
    JsonParse(#[from] serde_json::Error),

    /// sway replied to a `RUN_COMMAND` with `success = false`.
    #[error("sway rejected command: {0}")]
    CommandRejected(String),

    /// sway closed the named socket unexpectedly.
    #[error("sway closed the {kind} socket")]
    SocketClosed {
        /// Which socket role was closed.
        kind: SocketKind,
    },
}

/// Convenience alias for results produced by this crate.
pub type Result<T> = std::result::Result<T, Error>;
