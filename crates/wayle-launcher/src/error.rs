//! Launcher engine errors.

/// Errors produced by the launcher engine.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// History database failure.
    #[error("history database error: {0}")]
    Database(#[from] rusqlite::Error),

    /// Filesystem/IO failure (paths, mode sources).
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    /// Invalid user-supplied pattern (regex/glob matching methods).
    #[error("invalid pattern: {0}")]
    InvalidPattern(String),
}
