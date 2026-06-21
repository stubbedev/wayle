//! `org.freedesktop.impl.portal.GlobalShortcuts`.
//!
//! Bridges the portal interface to the compositor's
//! `hyprland-global-shortcuts-v1` (the de-facto wlroots mechanism, also used by
//! xdg-desktop-portal-hyprland). `BindShortcuts` registers each shortcut;
//! when the compositor triggers the bound key it reports a press/release that
//! becomes an `Activated`/`Deactivated` signal.
//!
//! Compositors without that protocol (currently niri, mango) accept binds but
//! never deliver activations — there is no portal-agnostic shortcut mechanism.

mod manager;

use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use tracing::{debug, warn};
use zbus::{
    Connection, interface,
    object_server::SignalEmitter,
    zvariant::{OwnedObjectPath, OwnedValue, Value},
};

use self::manager::{GsHandle, ShortcutEvent};
use crate::{dbus_util::opt_string, response::Response, session, settings::PORTAL_PATH};

/// A bound shortcut: its id and the properties echoed back to the app.
type Shortcut = (String, HashMap<String, OwnedValue>);

/// Per-session data.
#[derive(Clone, Default)]
struct GsSession {
    app_id: String,
    shortcuts: Vec<Shortcut>,
}

/// GlobalShortcuts portal interface.
pub struct GlobalShortcuts {
    connection: Connection,
    sessions: session::SessionStore<GsSession>,
    /// Lazily-started Wayland shortcuts manager.
    handle: Arc<Mutex<Option<GsHandle>>>,
    /// Maps a registration key to the session + shortcut id to signal.
    routes: Arc<Mutex<HashMap<String, (OwnedObjectPath, String)>>>,
}

impl GlobalShortcuts {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self {
            connection,
            sessions: session::SessionStore::default(),
            handle: Arc::new(Mutex::new(None)),
            routes: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Ensures the Wayland manager is running, starting it (and the activation
    /// signal task) on first use. Returns `false` if it is unavailable.
    ///
    /// The manager setup blocks on a Wayland roundtrip, so it runs on the
    /// blocking pool rather than the async D-Bus executor.
    async fn ensure_manager(&self) -> bool {
        if self
            .handle
            .lock()
            .map(|guard| guard.is_some())
            .unwrap_or(false)
        {
            return true;
        }
        match tokio::task::spawn_blocking(manager::spawn).await {
            Ok(Ok((handle, events))) => {
                self.spawn_signal_task(events);
                if let Ok(mut guard) = self.handle.lock() {
                    guard.get_or_insert(handle);
                }
                true
            }
            Ok(Err(err)) => {
                warn!(%err, "global shortcuts unavailable on this compositor");
                false
            }
            Err(err) => {
                warn!(%err, "global shortcuts manager task failed");
                false
            }
        }
    }

    /// Forwards compositor activations to `Activated`/`Deactivated` signals.
    fn spawn_signal_task(&self, mut events: tokio::sync::mpsc::UnboundedReceiver<ShortcutEvent>) {
        let connection = self.connection.clone();
        let routes = self.routes.clone();
        tokio::spawn(async move {
            let Ok(emitter) = SignalEmitter::new(&connection, PORTAL_PATH) else {
                warn!("global shortcuts: cannot build signal emitter");
                return;
            };
            while let Some(event) = events.recv().await {
                let Some((session, shortcut_id)) = routes
                    .lock()
                    .ok()
                    .and_then(|map| map.get(&event.key).cloned())
                else {
                    continue;
                };
                let result = if event.pressed {
                    GlobalShortcuts::activated(
                        &emitter,
                        session,
                        shortcut_id,
                        event.timestamp,
                        HashMap::new(),
                    )
                    .await
                } else {
                    GlobalShortcuts::deactivated(
                        &emitter,
                        session,
                        shortcut_id,
                        event.timestamp,
                        HashMap::new(),
                    )
                    .await
                };
                if let Err(err) = result {
                    debug!(%err, "global shortcuts: signal emit failed");
                }
            }
        });
    }
}

#[interface(name = "org.freedesktop.impl.portal.GlobalShortcuts")]
impl GlobalShortcuts {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Creates a session, recording the app id for shortcut registration.
    async fn create_session(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let sessions = self.sessions.clone();
        let routes = self.routes.clone();
        let key = session_handle.clone();
        let on_close = move || {
            sessions.remove(&key);
            // Forget routes that point at this session.
            if let Ok(mut map) = routes.lock() {
                map.retain(|_, (session, _)| session != &key);
            }
        };
        if let Err(err) = session::mount(&self.connection, &session_handle, on_close).await {
            warn!(%err, "global shortcuts: cannot mount session");
            return (Response::Other.code(), HashMap::new());
        }
        self.sessions.insert(
            session_handle,
            GsSession {
                app_id,
                shortcuts: Vec::new(),
            },
        );
        (Response::Success.code(), HashMap::new())
    }

    /// Registers shortcuts with the compositor.
    async fn bind_shortcuts(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        shortcuts: Vec<Shortcut>,
        _parent_window: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let session = self.sessions.get(&session_handle).unwrap_or_default();
        let available = self.ensure_manager().await;

        if available
            && let Ok(guard) = self.handle.lock()
            && let Some(handle) = guard.as_ref()
        {
            for (id, props) in &shortcuts {
                let key = route_key(&session.app_id, id);
                let description = opt_string(props, "description").unwrap_or_default();
                let trigger = opt_string(props, "preferred_trigger").unwrap_or_default();
                handle.register(key.clone(), id, &session.app_id, &description, &trigger);
                if let Ok(mut map) = self.routes.lock() {
                    map.insert(key, (session_handle.clone(), id.clone()));
                }
            }
        }

        self.sessions.update(&session_handle, |data| {
            data.shortcuts = shortcuts.clone();
        });

        let mut results = HashMap::new();
        if let Ok(value) = shortcuts_value(&shortcuts) {
            results.insert("shortcuts".to_owned(), value);
        }
        (Response::Success.code(), results)
    }

    /// Lists the shortcuts bound on this session.
    async fn list_shortcuts(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let session = self.sessions.get(&session_handle).unwrap_or_default();
        let mut results = HashMap::new();
        if let Ok(value) = shortcuts_value(&session.shortcuts) {
            results.insert("shortcuts".to_owned(), value);
        }
        (Response::Success.code(), results)
    }

    /// Emitted when a bound shortcut is pressed.
    #[zbus(signal)]
    async fn activated(
        emitter: &SignalEmitter<'_>,
        session_handle: OwnedObjectPath,
        shortcut_id: String,
        timestamp: u64,
        options: HashMap<String, OwnedValue>,
    ) -> zbus::Result<()>;

    /// Emitted when a bound shortcut is released.
    #[zbus(signal)]
    async fn deactivated(
        emitter: &SignalEmitter<'_>,
        session_handle: OwnedObjectPath,
        shortcut_id: String,
        timestamp: u64,
        options: HashMap<String, OwnedValue>,
    ) -> zbus::Result<()>;
}

/// Registration key tying a compositor shortcut to a session route.
fn route_key(app_id: &str, id: &str) -> String {
    format!("{app_id}\u{1f}{id}")
}

/// Encodes shortcuts as the `a(sa{sv})` results value.
fn shortcuts_value(shortcuts: &[Shortcut]) -> Result<OwnedValue, zbus::zvariant::Error> {
    let cloned: Vec<Shortcut> = shortcuts
        .iter()
        .map(|(id, props)| (id.clone(), clone_props(props)))
        .collect();
    OwnedValue::try_from(Value::from(cloned))
}

/// Deep-clones a property vardict.
fn clone_props(props: &HashMap<String, OwnedValue>) -> HashMap<String, OwnedValue> {
    props
        .iter()
        .filter_map(|(k, v)| Some((k.clone(), v.try_clone().ok()?)))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn route_key_combines_app_and_id() {
        assert_eq!(route_key("app.A", "toggle"), "app.A\u{1f}toggle");
        // Different app/id pairs (no separator in inputs) yield different keys.
        assert_ne!(route_key("a", "b"), route_key("ab", ""));
    }
}
