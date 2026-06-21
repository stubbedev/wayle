//! `org.freedesktop.impl.portal.InputCapture`.
//!
//! Real input *capture* (barrier grab + an EIS socket) needs the compositor to
//! redirect local input to the portal — wlroots compositors and niri do not
//! expose that to a portal (only KWin/Mutter back it). So this interface is
//! present and introspectable, reports zero capabilities (clients detect
//! non-support and fall back), and answers `GetZones` truthfully from the
//! output layout, but performs no grab. This matches reality on wlroots without
//! pretending to capture.

use std::collections::HashMap;

use tracing::warn;
use wayland_client::Connection;
use wayle_share_preview::output::OutputManager;
use zbus::{
    Connection as ZbusConnection, interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{dbus_util::owned, response::Response, session};

/// A capture zone: `(width, height, x_offset, y_offset)`.
type Zone = (u32, u32, i32, i32);

/// InputCapture portal interface.
pub struct InputCapture {
    connection: ZbusConnection,
    sessions: session::SessionStore<()>,
}

impl InputCapture {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: ZbusConnection) -> Self {
        Self {
            connection,
            sessions: session::SessionStore::default(),
        }
    }

    async fn mount(&self, session_handle: &OwnedObjectPath) -> bool {
        let sessions = self.sessions.clone();
        let key = session_handle.clone();
        let on_close = move || {
            sessions.remove(&key);
        };
        if let Err(err) = session::mount(&self.connection, session_handle, on_close).await {
            warn!(%err, "inputcapture: cannot mount session");
            return false;
        }
        self.sessions.insert(session_handle.clone(), ());
        true
    }
}

#[interface(name = "org.freedesktop.impl.portal.InputCapture")]
impl InputCapture {
    /// Capabilities we can capture: none (no compositor grab on wlroots).
    #[zbus(property, name = "SupportedCapabilities")]
    fn supported_capabilities(&self) -> u32 {
        0
    }

    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Creates a session (v1).
    async fn create_session(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        if !self.mount(&session_handle).await {
            return (Response::Other.code(), HashMap::new());
        }
        (Response::Success.code(), capabilities_results())
    }

    /// Creates a session (v2 — no request handle, returns only results).
    async fn create_session2(
        &self,
        session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> HashMap<String, OwnedValue> {
        self.mount(&session_handle).await;
        capabilities_results()
    }

    /// Starts the session. No-op success; nothing is captured.
    async fn start(
        &self,
        _handle: OwnedObjectPath,
        _session_handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        (Response::Success.code(), HashMap::new())
    }

    /// Reports the output layout as capture zones.
    async fn get_zones(
        &self,
        _handle: OwnedObjectPath,
        _session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let zones = tokio::task::spawn_blocking(collect_zones)
            .await
            .unwrap_or_default();
        let mut results = HashMap::new();
        if let Some(value) = owned(zones) {
            results.insert("zones".to_owned(), value);
        }
        if let Some(value) = owned(1u32) {
            results.insert("zone_set".to_owned(), value);
        }
        (Response::Success.code(), results)
    }

    /// Accepts barriers but reports them all as failed (no grab available).
    async fn set_pointer_barriers(
        &self,
        _handle: OwnedObjectPath,
        _session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
        barriers: Vec<HashMap<String, OwnedValue>>,
        _zone_set: u32,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let failed: Vec<u32> = barriers
            .iter()
            .filter_map(|b| b.get("barrier_id").and_then(|v| u32::try_from(v).ok()))
            .collect();
        let mut results = HashMap::new();
        if let Some(value) = owned(failed) {
            results.insert("failed_barriers".to_owned(), value);
        }
        (Response::Success.code(), results)
    }

    /// No-op (nothing to enable).
    async fn enable(
        &self,
        _session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        (Response::Success.code(), HashMap::new())
    }

    /// No-op.
    async fn disable(
        &self,
        _session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        (Response::Success.code(), HashMap::new())
    }

    /// No-op.
    async fn release(
        &self,
        _session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        (Response::Success.code(), HashMap::new())
    }
}

/// Results vardict advertising zero session capabilities.
fn capabilities_results() -> HashMap<String, OwnedValue> {
    owned(0u32)
        .map(|v| HashMap::from([("capabilities".to_owned(), v)]))
        .unwrap_or_default()
}

/// Collects the output layout as `(width, height, x, y)` zones.
fn collect_zones() -> Vec<Zone> {
    let Ok(connection) = Connection::connect_to_env() else {
        return Vec::new();
    };
    let Ok(manager) = OutputManager::new(&connection) else {
        return Vec::new();
    };
    manager
        .outputs
        .iter()
        .filter_map(|(_, output)| {
            let mode = output.mode.as_ref()?;
            let geometry = output.geometry.as_ref()?;
            let width = u32::try_from(mode.width).ok()?;
            let height = u32::try_from(mode.height).ok()?;
            Some((width, height, geometry.x, geometry.y))
        })
        .collect()
}
