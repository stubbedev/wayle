//! Screenshot service exposing `com.wayle.Screenshot1` on the session bus.
//!
//! The D-Bus method bridges to the GTK-thread [`Screenshot`] host through a
//! process-global Relm4 sender, registered by the shell once its UI is built
//! (see [`register_sender`]).
//!
//! [`Screenshot`]: crate::shell::screenshot::Screenshot

mod dbus;

use std::sync::OnceLock;

use relm4::Sender;
use tracing::info;
use wayle_ipc::screenshot::{SERVICE_NAME, SERVICE_PATH};
use zbus::Connection;

use self::dbus::ScreenshotDaemon;
use crate::shell::screenshot::ScreenshotInput;

/// GTK-thread sender into the screenshot host. Set once the shell UI exists.
static HOST_SENDER: OnceLock<Sender<ScreenshotInput>> = OnceLock::new();

/// Records the host's input sender so the D-Bus handler can reach it. Called
/// once during shell init; later calls are ignored.
pub(crate) fn register_sender(sender: Sender<ScreenshotInput>) {
    if HOST_SENDER.set(sender).is_err() {
        tracing::warn!("screenshot host sender already registered");
    }
}

/// Returns a clone of the registered host sender, if the UI is ready.
pub(crate) fn host_sender() -> Option<Sender<ScreenshotInput>> {
    HOST_SENDER.get().cloned()
}

/// Keeps the registered service (and its D-Bus connection) alive.
static SERVICE: OnceLock<ScreenshotService> = OnceLock::new();

/// Registers the screenshot D-Bus interface and keeps it alive.
///
/// Non-fatal: callers should log and continue if this fails, leaving the shell
/// usable without screenshot support.
///
/// # Errors
///
/// Returns an error if the session bus connection or D-Bus registration fails.
pub async fn start() -> Result<(), Error> {
    let service = ScreenshotService::new().await?;
    let _ = SERVICE.set(service);
    Ok(())
}

/// Registers the `com.wayle.Screenshot1` D-Bus interface.
pub struct ScreenshotService {
    _connection: Connection,
}

impl ScreenshotService {
    /// Connects to the session bus and registers the interface.
    ///
    /// # Errors
    ///
    /// Returns an error if the session bus is unreachable, the object cannot be
    /// registered, or the D-Bus name is already claimed.
    pub async fn new() -> Result<Self, Error> {
        let connection = Connection::session()
            .await
            .map_err(|e| Error::Connection(e.to_string()))?;

        connection
            .object_server()
            .at(SERVICE_PATH, ScreenshotDaemon)
            .await
            .map_err(|e| Error::Registration(e.to_string()))?;

        connection
            .request_name(SERVICE_NAME)
            .await
            .map_err(|e| Error::NameRequest(e.to_string()))?;

        info!("Screenshot service registered at {SERVICE_NAME}");

        Ok(Self {
            _connection: connection,
        })
    }
}

/// Errors from screenshot service initialization.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// Could not connect to the session bus.
    #[error("cannot connect to session bus")]
    Connection(String),

    /// Could not register the D-Bus object.
    #[error("cannot register D-Bus object")]
    Registration(String),

    /// Could not request the D-Bus name.
    #[error("cannot request D-Bus name")]
    NameRequest(String),
}
