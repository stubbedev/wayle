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
use crate::{
    dbus_util::opt_string,
    protocol::hyprland_global_shortcuts_v1::hyprland_global_shortcut_v1::HyprlandGlobalShortcutV1,
    response::Response,
    session,
    settings::PORTAL_PATH,
};

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
    /// Compositor objects keyed by registration key, so a re-bind of an
    /// existing `app_id`+`id` reuses the object instead of minting (and
    /// leaking) a new one. The compositor would otherwise raise
    /// `already_taken` for a duplicate pair.
    objects: Arc<Mutex<HashMap<String, HyprlandGlobalShortcutV1>>>,
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
            objects: Arc::new(Mutex::new(HashMap::new())),
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

    /// Registers a batch of shortcuts with the compositor for a session,
    /// reusing any that are already registered for this `app_id`+`id` pair.
    ///
    /// Shared by `CreateSession` (shortcuts passed in options) and
    /// `BindShortcuts`. Re-binding an already-registered id does NOT mint a new
    /// compositor object — that would leak the old one and trip the protocol's
    /// `already_taken` error — it reuses the existing object and route.
    async fn register_shortcuts(
        &self,
        session_handle: &OwnedObjectPath,
        app_id: &str,
        shortcuts: &[Shortcut],
    ) {
        if shortcuts.is_empty() || !self.ensure_manager().await {
            return;
        }

        if let Ok(guard) = self.handle.lock()
            && let Some(handle) = guard.as_ref()
        {
            for (id, props) in shortcuts {
                let key = route_key(app_id, id);

                // Reuse an existing registration; only the first bind of an
                // id mints a compositor object (cf. xdph getShortcutById).
                if self
                    .objects
                    .lock()
                    .map(|map| map.contains_key(&key))
                    .unwrap_or(false)
                {
                    continue;
                }

                let description = opt_string(props, "description").unwrap_or_default();
                let trigger = opt_string(props, "preferred_trigger").unwrap_or_default();
                let object = handle.register(key.clone(), id, app_id, &description, &trigger);

                if let Ok(mut map) = self.routes.lock() {
                    map.insert(key.clone(), (session_handle.clone(), id.clone()));
                }
                if let Ok(mut map) = self.objects.lock() {
                    map.insert(key, object);
                }
            }
        }
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
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let sessions = self.sessions.clone();
        let routes = self.routes.clone();
        let objects = self.objects.clone();
        let key = session_handle.clone();
        let on_close = move || {
            sessions.remove(&key);
            // Forget routes that point at this session and destroy the
            // compositor objects they referenced.
            let stale: Vec<String> = routes
                .lock()
                .map(|mut map| {
                    let stale: Vec<String> = map
                        .iter()
                        .filter(|(_, (session, _))| session == &key)
                        .map(|(route_key, _)| route_key.clone())
                        .collect();
                    map.retain(|_, (session, _)| session != &key);
                    stale
                })
                .unwrap_or_default();
            if let Ok(mut map) = objects.lock() {
                for route_key in stale {
                    if let Some(object) = map.remove(&route_key) {
                        object.destroy();
                    }
                }
            }
        };
        if let Err(err) = session::mount(&self.connection, &session_handle, on_close).await {
            warn!(%err, "global shortcuts: cannot mount session");
            return (Response::Other.code(), HashMap::new());
        }

        // xdph registers shortcuts passed at CreateSession time.
        let shortcuts = parse_shortcuts(options.get("shortcuts")).unwrap_or_default();
        self.sessions.insert(
            session_handle.clone(),
            GsSession {
                app_id: app_id.clone(),
                shortcuts: shortcuts.clone(),
            },
        );
        self.register_shortcuts(&session_handle, &app_id, &shortcuts)
            .await;
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

        self.register_shortcuts(&session_handle, &session.app_id, &shortcuts)
            .await;

        // Merge the newly-bound shortcuts into the session, replacing the
        // props of any id already present and appending the rest.
        self.sessions.update(&session_handle, |data| {
            merge_shortcuts(&mut data.shortcuts, &shortcuts);
        });

        let merged = self.sessions.get(&session_handle).unwrap_or_default();
        let mut results = HashMap::new();
        if let Ok(value) = shortcuts_value(&merged.shortcuts) {
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

    /// Emitted when the set of bound shortcuts (or their triggers) changes.
    ///
    /// Declared to match the xdg-desktop-portal-hyprland interface surface;
    /// wayle does not currently emit it (the compositor does not report
    /// trigger changes), but the signal must be present on the interface.
    #[zbus(signal)]
    async fn shortcuts_changed(
        emitter: &SignalEmitter<'_>,
        session_handle: OwnedObjectPath,
        shortcuts: Vec<Shortcut>,
    ) -> zbus::Result<()>;
}

/// Registration key tying a compositor shortcut to a session route.
fn route_key(app_id: &str, id: &str) -> String {
    format!("{app_id}\u{1f}{id}")
}

/// Decodes the `shortcuts` option (an `a(sa{sv})`) passed to `CreateSession`.
///
/// Returns `None` when the option is absent or not of the expected type, so
/// the caller can treat a missing/garbled value as "no shortcuts".
fn parse_shortcuts(value: Option<&OwnedValue>) -> Option<Vec<Shortcut>> {
    let value = value?.try_clone().ok()?;
    Vec::<Shortcut>::try_from(value).ok()
}

/// Merges `incoming` shortcuts into `existing`, replacing the props of any id
/// already present and appending ids that are new. Mirrors xdph treating a
/// re-bind of an existing id as an update rather than a duplicate.
fn merge_shortcuts(existing: &mut Vec<Shortcut>, incoming: &[Shortcut]) {
    for (id, props) in incoming {
        if let Some(slot) = existing.iter_mut().find(|(existing_id, _)| existing_id == id) {
            slot.1 = clone_props(props);
        } else {
            existing.push((id.clone(), clone_props(props)));
        }
    }
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

    /// Builds a single-entry `{description: <desc>}` property vardict.
    fn props(desc: &str) -> HashMap<String, OwnedValue> {
        let mut map = HashMap::new();
        let value = OwnedValue::try_from(Value::from(desc)).unwrap();
        map.insert("description".to_owned(), value);
        map
    }

    fn desc_of(shortcut: &Shortcut) -> String {
        opt_string(&shortcut.1, "description").unwrap_or_default()
    }

    #[test]
    fn merge_shortcuts_appends_new_ids() {
        let mut existing = vec![("a".to_owned(), props("first"))];
        let incoming = vec![("b".to_owned(), props("second"))];
        merge_shortcuts(&mut existing, &incoming);

        assert_eq!(existing.len(), 2);
        assert_eq!(existing[0].0, "a");
        assert_eq!(existing[1].0, "b");
        assert_eq!(desc_of(&existing[1]), "second");
    }

    #[test]
    fn merge_shortcuts_replaces_existing_id_in_place() {
        let mut existing = vec![
            ("a".to_owned(), props("old-a")),
            ("b".to_owned(), props("b")),
        ];
        let incoming = vec![("a".to_owned(), props("new-a"))];
        merge_shortcuts(&mut existing, &incoming);

        // No duplicate id, updated props, original order preserved.
        assert_eq!(existing.len(), 2);
        assert_eq!(existing[0].0, "a");
        assert_eq!(desc_of(&existing[0]), "new-a");
        assert_eq!(existing[1].0, "b");
    }

    #[test]
    fn merge_shortcuts_empty_incoming_is_noop() {
        let mut existing = vec![("a".to_owned(), props("a"))];
        merge_shortcuts(&mut existing, &[]);
        assert_eq!(existing.len(), 1);
        assert_eq!(existing[0].0, "a");
    }

    #[test]
    fn parse_shortcuts_none_when_absent() {
        assert!(parse_shortcuts(None).is_none());
    }

    #[test]
    fn parse_shortcuts_none_on_wrong_type() {
        let wrong = OwnedValue::try_from(Value::from("not-an-array")).unwrap();
        assert!(parse_shortcuts(Some(&wrong)).is_none());
    }

    #[test]
    fn parse_shortcuts_round_trips_encoded_value() {
        let shortcuts = vec![
            ("toggle".to_owned(), props("Toggle mic")),
            ("next".to_owned(), props("Next track")),
        ];
        let encoded = shortcuts_value(&shortcuts).unwrap();
        let decoded = parse_shortcuts(Some(&encoded)).unwrap();

        assert_eq!(decoded.len(), 2);
        assert_eq!(decoded[0].0, "toggle");
        assert_eq!(desc_of(&decoded[0]), "Toggle mic");
        assert_eq!(decoded[1].0, "next");
        assert_eq!(desc_of(&decoded[1]), "Next track");
    }
}
