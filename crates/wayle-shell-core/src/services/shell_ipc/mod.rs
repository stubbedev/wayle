//! Shell IPC service exposing `com.wayle.Shell1` on the session bus.
//!
//! Provides bar visibility control (hide/show/toggle per monitor) via
//! D-Bus methods, and reactive [`ShellIpcState`] that bar components
//! watch to apply visibility changes.

mod bar;
mod dbus;
mod error;
mod state;

pub use error::Error;
pub use state::ShellIpcState;
use tracing::info;
use wayle_ipc::shell_ipc::{SERVICE_NAME, SERVICE_PATH};
use zbus::Connection;

use self::dbus::ShellIpcDaemon;

/// Hook the shell installs so the D-Bus `Lock` method can reach the lock
/// window component, which stays in wayle-shell.
static LOCK_TRIGGER: std::sync::OnceLock<fn() -> bool> = std::sync::OnceLock::new();

/// Installs the lock-screen handler. Called once by wayle-shell at startup;
/// later calls are ignored.
pub fn set_lock_trigger(trigger: fn() -> bool) {
    let _ = LOCK_TRIGGER.set(trigger);
}

pub(crate) fn lock_trigger() -> fn() -> bool {
    *LOCK_TRIGGER.get().unwrap_or(&((|| false) as fn() -> bool))
}

/// Registers the `com.wayle.Shell1` D-Bus interface and holds the
/// [`ShellIpcState`] that bar components watch for visibility changes.
pub struct ShellIpcService {
    state: ShellIpcState,
    _connection: Connection,
}

impl ShellIpcService {
    /// Connects to the session bus and registers the `com.wayle.Shell1` interface.
    ///
    /// # Errors
    ///
    /// Returns an error if the session bus is unreachable or the D-Bus name
    /// is already claimed.
    pub async fn new() -> Result<Self, Error> {
        let state = ShellIpcState::new();

        let connection = Connection::session()
            .await
            .map_err(|err| Error::Connection(err.to_string()))?;

        let daemon = ShellIpcDaemon::new(state.clone());

        connection
            .object_server()
            .at(SERVICE_PATH, daemon)
            .await
            .map_err(|err| Error::Registration(err.to_string()))?;

        connection
            .request_name(SERVICE_NAME)
            .await
            .map_err(|err| Error::NameRequest(err.to_string()))?;

        info!("Shell IPC service registered at {SERVICE_NAME}");

        Ok(Self {
            state,
            _connection: connection,
        })
    }

    /// Reactive state that bar components subscribe to for visibility updates.
    pub fn state(&self) -> ShellIpcState {
        self.state.clone()
    }
}
