//! Portal dialog service exposing `com.wayle.PortalDialogs1` on the session bus.
//!
//! Bridges the portal backend's Access / Account / AppChooser / DynamicLauncher
//! interfaces to the GTK-thread [`PortalDialogs`] host.
//!
//! [`PortalDialogs`]: crate::shell::portal_dialogs::PortalDialogs

mod dbus;

use std::sync::OnceLock;

use relm4::Sender;
use tracing::info;
use wayle_ipc::portal_dialogs::{SERVICE_NAME, SERVICE_PATH};
use zbus::Connection;

use self::dbus::PortalDialogsDaemon;
use crate::shell::portal_dialogs::PortalDialogInput;

static HOST_SENDER: OnceLock<Sender<PortalDialogInput>> = OnceLock::new();

/// Records the host's input sender so the D-Bus handler can reach it.
pub(crate) fn register_sender(sender: Sender<PortalDialogInput>) {
    if HOST_SENDER.set(sender).is_err() {
        tracing::warn!("portal dialogs host sender already registered");
    }
}

/// Returns a clone of the registered host sender, if the UI is ready.
pub(crate) fn host_sender() -> Option<Sender<PortalDialogInput>> {
    HOST_SENDER.get().cloned()
}

static SERVICE: OnceLock<PortalDialogsService> = OnceLock::new();

/// Registers the dialog D-Bus interface and keeps it alive. Non-fatal.
///
/// # Errors
///
/// Returns an error if the session bus connection or D-Bus registration fails.
pub async fn start() -> Result<(), Error> {
    let service = PortalDialogsService::new().await?;
    let _ = SERVICE.set(service);
    Ok(())
}

/// Registers the `com.wayle.PortalDialogs1` D-Bus interface.
pub struct PortalDialogsService {
    _connection: Connection,
}

impl PortalDialogsService {
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
            .at(SERVICE_PATH, PortalDialogsDaemon)
            .await
            .map_err(|e| Error::Registration(e.to_string()))?;

        connection
            .request_name(SERVICE_NAME)
            .await
            .map_err(|e| Error::NameRequest(e.to_string()))?;

        info!("Portal dialogs service registered at {SERVICE_NAME}");

        Ok(Self {
            _connection: connection,
        })
    }
}

/// Errors from portal dialog service initialization.
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
