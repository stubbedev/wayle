//! Tuning values and well-known names used across the crate.

/// Environment variable sway sets to the path of its IPC socket.
pub(crate) const SOCKET_PATH_ENV: &str = "SWAYSOCK";

/// Capacity of the public broadcast event channel.
///
/// When a subscriber lags past this many events, tokio's broadcast channel
/// reports `RecvError::Lagged(n)` so the receiver can decide what to do.
pub(crate) const EVENT_CHANNEL_CAPACITY: usize = 100;
