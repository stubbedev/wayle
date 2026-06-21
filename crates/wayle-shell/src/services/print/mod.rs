//! Print service exposing `com.wayle.Print1` on the session bus.
//!
//! Bridges the portal backend's `org.freedesktop.impl.portal.Print` to the
//! GTK-thread [`Print`] host.
//!
//! [`Print`]: crate::shell::print::Print

mod dbus;

use std::sync::OnceLock;

use relm4::Sender;
use tracing::info;
use wayle_ipc::print::{SERVICE_NAME, SERVICE_PATH};
use zbus::Connection;

use self::dbus::PrintDaemon;
use crate::shell::print::PrintInput;

static HOST_SENDER: OnceLock<Sender<PrintInput>> = OnceLock::new();

/// Records the host's input sender so the D-Bus handler can reach it.
pub(crate) fn register_sender(sender: Sender<PrintInput>) {
    if HOST_SENDER.set(sender).is_err() {
        tracing::warn!("print host sender already registered");
    }
}

/// Returns a clone of the registered host sender, if the UI is ready.
pub(crate) fn host_sender() -> Option<Sender<PrintInput>> {
    HOST_SENDER.get().cloned()
}

static SERVICE: OnceLock<PrintService> = OnceLock::new();

/// Registers the print D-Bus interface and keeps it alive. Non-fatal.
///
/// # Errors
///
/// Returns an error if the session bus connection or D-Bus registration fails.
pub async fn start() -> Result<(), Error> {
    let service = PrintService::new().await?;
    let _ = SERVICE.set(service);
    Ok(())
}

/// Registers the `com.wayle.Print1` D-Bus interface.
pub struct PrintService {
    _connection: Connection,
}

impl PrintService {
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
            .at(SERVICE_PATH, PrintDaemon)
            .await
            .map_err(|e| Error::Registration(e.to_string()))?;

        connection
            .request_name(SERVICE_NAME)
            .await
            .map_err(|e| Error::NameRequest(e.to_string()))?;

        info!("Print service registered at {SERVICE_NAME}");

        Ok(Self {
            _connection: connection,
        })
    }
}

/// Errors from print service initialization.
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
