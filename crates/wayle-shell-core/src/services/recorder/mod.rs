//! Screen recorder service with D-Bus control interface.

mod dbus;
mod state;

use std::sync::Arc;

use dbus::{RecorderDaemon, SERVICE_NAME, SERVICE_PATH};
pub use state::RecorderState;
use tracing::info;
use wayle_config::ConfigService;
use wayle_recorder::Recorder;
use zbus::Connection;

use crate::services::widget_ipc::ToastBus;

/// Screen recorder service.
///
/// Owns the GStreamer engine + shared reactive state and exposes control over
/// D-Bus at `com.wayle.Recorder1` (driven by `wayle recorder ...`).
pub struct RecorderService {
    state: RecorderState,
    _connection: Connection,
}

impl RecorderService {
    /// Creates the service, initializing GStreamer and registering D-Bus.
    ///
    /// # Errors
    ///
    /// Returns [`Error`] if GStreamer init, the session bus connection, or the
    /// D-Bus registration fails.
    pub async fn new(config: Arc<ConfigService>, toast_bus: ToastBus) -> Result<Self, Error> {
        let recorder = Recorder::new().map_err(|e| Error::Engine(e.to_string()))?;
        let state = RecorderState::new(Arc::new(recorder), config, toast_bus);

        let connection = Connection::session()
            .await
            .map_err(|e| Error::Connection(e.to_string()))?;

        let daemon = RecorderDaemon::new(state.clone());

        connection
            .object_server()
            .at(SERVICE_PATH, daemon)
            .await
            .map_err(|e| Error::Registration(e.to_string()))?;

        connection
            .request_name(SERVICE_NAME)
            .await
            .map_err(|e| Error::NameRequest(e.to_string()))?;

        info!("Recorder service registered at {SERVICE_NAME}");

        Ok(Self {
            state,
            _connection: connection,
        })
    }

    /// Returns a clone of the shared state for modules to watch.
    pub fn state(&self) -> RecorderState {
        self.state.clone()
    }
}

/// Errors from recorder service initialization.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("recorder engine init failed")]
    Engine(String),

    #[error("cannot connect to session bus")]
    Connection(String),

    #[error("cannot register D-Bus object")]
    Registration(String),

    #[error("cannot request D-Bus name")]
    NameRequest(String),
}
