//! Share picker service exposing `com.wayle.SharePicker1` on the session bus.
//!
//! The D-Bus method bridges to the GTK-thread [`SharePicker`] component
//! through a process-global Relm4 sender, registered by the shell once its UI
//! is built (see [`register_sender`]).
//!
//! [`SharePicker`]: crate::shell::share_picker::SharePicker

mod dbus;

use std::sync::OnceLock;

use relm4::Sender;
use tracing::info;
use wayle_ipc::share_picker::{SERVICE_NAME, SERVICE_PATH};
use zbus::Connection;

use self::dbus::SharePickerDaemon;
use crate::shell::share_picker::SharePickerInput;

/// GTK-thread sender into the picker component. Set once the shell UI exists;
/// the D-Bus handler reads it lazily when a request arrives.
static PICKER_SENDER: OnceLock<Sender<SharePickerInput>> = OnceLock::new();

/// Records the picker component's input sender so the D-Bus handler can reach
/// it. Called once during shell init; later calls are ignored.
pub(crate) fn register_sender(sender: Sender<SharePickerInput>) {
    if PICKER_SENDER.set(sender).is_err() {
        tracing::warn!("share picker sender already registered");
    }
}

/// Returns a clone of the registered picker sender, if the UI is ready.
pub(super) fn picker_sender() -> Option<Sender<SharePickerInput>> {
    PICKER_SENDER.get().cloned()
}

/// Keeps the registered service (and its D-Bus connection) alive for the
/// lifetime of the process.
static SERVICE: OnceLock<SharePickerService> = OnceLock::new();

/// Registers the share picker D-Bus interface and keeps it alive.
///
/// Non-fatal: callers should log and continue if this fails, leaving the
/// shell usable without the custom picker.
///
/// # Errors
///
/// Returns an error if the session bus connection or D-Bus registration fails.
pub async fn start() -> Result<(), Error> {
    let service = SharePickerService::new().await?;
    let _ = SERVICE.set(service);
    Ok(())
}

/// Registers the `com.wayle.SharePicker1` D-Bus interface.
pub struct SharePickerService {
    _connection: Connection,
}

impl SharePickerService {
    /// Connects to the session bus and registers the interface.
    ///
    /// # Errors
    ///
    /// Returns an error if the session bus is unreachable, the object cannot
    /// be registered, or the D-Bus name is already claimed.
    pub async fn new() -> Result<Self, Error> {
        let connection = Connection::session()
            .await
            .map_err(|e| Error::Connection(e.to_string()))?;

        connection
            .object_server()
            .at(SERVICE_PATH, SharePickerDaemon)
            .await
            .map_err(|e| Error::Registration(e.to_string()))?;

        connection
            .request_name(SERVICE_NAME)
            .await
            .map_err(|e| Error::NameRequest(e.to_string()))?;

        info!("Share picker service registered at {SERVICE_NAME}");

        Ok(Self {
            _connection: connection,
        })
    }
}

/// Errors from share picker service initialization.
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
