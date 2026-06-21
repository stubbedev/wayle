//! `org.freedesktop.impl.portal.Clipboard`.
//!
//! Bridges the Wayland selection (`zwlr_data_control`) to clipboard-enabled
//! RemoteDesktop sessions: apps read the current selection, become the
//! selection owner, and serve their data on demand. The Wayland work lives in
//! [`manager`]; this is the D-Bus surface + signal fan-out.

mod manager;

use std::{
    collections::HashSet,
    sync::{Arc, Mutex},
};

use tracing::{debug, warn};
use zbus::{
    Connection, interface,
    object_server::SignalEmitter,
    zvariant::{OwnedFd, OwnedObjectPath, OwnedValue},
};

use self::manager::{ClipEvent, ClipboardHandle};
use crate::{dbus_util::opt_string, settings::PORTAL_PATH};

/// Clipboard portal interface.
pub struct Clipboard {
    connection: Connection,
    handle: Arc<Mutex<Option<ClipboardHandle>>>,
    /// Sessions that called `RequestClipboard` (receive selection signals).
    enabled: Arc<Mutex<HashSet<OwnedObjectPath>>>,
}

impl Clipboard {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self {
            connection,
            handle: Arc::new(Mutex::new(None)),
            enabled: Arc::new(Mutex::new(HashSet::new())),
        }
    }

    /// Starts the Wayland bridge on first use; returns whether it's available.
    async fn ensure(&self) -> bool {
        if self.handle.lock().map(|g| g.is_some()).unwrap_or(false) {
            return true;
        }
        match tokio::task::spawn_blocking(manager::spawn).await {
            Ok(Ok((handle, events))) => {
                self.spawn_event_task(events);
                if let Ok(mut guard) = self.handle.lock() {
                    guard.get_or_insert(handle);
                }
                true
            }
            Ok(Err(err)) => {
                warn!(%err, "clipboard unavailable on this compositor");
                false
            }
            Err(err) => {
                warn!(%err, "clipboard manager task failed");
                false
            }
        }
    }

    /// Fans selection events out to every clipboard-enabled session.
    fn spawn_event_task(&self, mut events: tokio::sync::mpsc::UnboundedReceiver<ClipEvent>) {
        let connection = self.connection.clone();
        let enabled = self.enabled.clone();
        tokio::spawn(async move {
            let Ok(emitter) = SignalEmitter::new(&connection, PORTAL_PATH) else {
                return;
            };
            while let Some(event) = events.recv().await {
                let sessions: Vec<OwnedObjectPath> = enabled
                    .lock()
                    .map(|s| s.iter().cloned().collect())
                    .unwrap_or_default();
                for session in sessions {
                    let result = match &event {
                        ClipEvent::OwnerChanged => {
                            Clipboard::selection_owner_changed(
                                &emitter,
                                session,
                                std::collections::HashMap::new(),
                            )
                            .await
                        }
                        ClipEvent::Transfer { mime, serial } => {
                            Clipboard::selection_transfer(&emitter, session, mime, *serial).await
                        }
                    };
                    if let Err(err) = result {
                        debug!(%err, "clipboard: signal emit failed");
                    }
                }
            }
        });
    }

    /// Runs a closure with the live handle, if the bridge is up.
    fn with_handle<T>(&self, f: impl FnOnce(&ClipboardHandle) -> T) -> Option<T> {
        let guard = self.handle.lock().ok()?;
        guard.as_ref().map(f)
    }
}

#[interface(name = "org.freedesktop.impl.portal.Clipboard")]
impl Clipboard {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Enables clipboard access for a (RemoteDesktop) session.
    async fn request_clipboard(
        &self,
        session_handle: OwnedObjectPath,
        _options: std::collections::HashMap<String, OwnedValue>,
    ) {
        if self.ensure().await
            && let Ok(mut enabled) = self.enabled.lock()
        {
            enabled.insert(session_handle);
        }
    }

    /// Becomes the selection owner offering the given mime types.
    async fn set_selection(
        &self,
        _session_handle: OwnedObjectPath,
        options: std::collections::HashMap<String, OwnedValue>,
    ) {
        let mimes = mime_list(&options);
        self.with_handle(|handle| handle.set_selection(&mimes));
    }

    /// Returns a writable fd for a pending transfer the app must fill.
    async fn selection_write(
        &self,
        _session_handle: OwnedObjectPath,
        serial: u32,
    ) -> zbus::fdo::Result<OwnedFd> {
        self.with_handle(|handle| handle.take_transfer_fd(serial))
            .flatten()
            .map(OwnedFd::from)
            .ok_or_else(|| zbus::fdo::Error::Failed("no pending clipboard transfer".to_owned()))
    }

    /// Acknowledges a finished transfer (the fd was already handed to the app).
    async fn selection_write_done(
        &self,
        _session_handle: OwnedObjectPath,
        _serial: u32,
        _success: bool,
    ) {
    }

    /// Returns a readable fd streaming the current selection's `mime` content.
    async fn selection_read(
        &self,
        _session_handle: OwnedObjectPath,
        mime_type: String,
    ) -> zbus::fdo::Result<OwnedFd> {
        self.with_handle(|handle| handle.read(&mime_type))
            .flatten()
            .map(OwnedFd::from)
            .ok_or_else(|| zbus::fdo::Error::Failed("no clipboard selection".to_owned()))
    }

    /// Emitted when the selection owner changes.
    #[zbus(signal)]
    async fn selection_owner_changed(
        emitter: &SignalEmitter<'_>,
        session_handle: OwnedObjectPath,
        options: std::collections::HashMap<String, OwnedValue>,
    ) -> zbus::Result<()>;

    /// Emitted to request the owned selection's data for a mime type.
    #[zbus(signal)]
    async fn selection_transfer(
        emitter: &SignalEmitter<'_>,
        session_handle: OwnedObjectPath,
        mime_type: &str,
        serial: u32,
    ) -> zbus::Result<()>;
}

/// Extracts the `mime_types` (`as`) option.
fn mime_list(options: &std::collections::HashMap<String, OwnedValue>) -> Vec<String> {
    options
        .get("mime_types")
        .and_then(|v| Vec::<String>::try_from(v.try_clone().ok()?).ok())
        .or_else(|| opt_string(options, "mime_type").map(|m| vec![m]))
        .unwrap_or_default()
}
