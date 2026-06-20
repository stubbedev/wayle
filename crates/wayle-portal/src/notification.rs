//! `org.freedesktop.impl.portal.Notification`.
//!
//! Translates portal notifications into `org.freedesktop.Notifications.Notify`
//! calls, which the running shell's notification daemon (wayle-notification)
//! displays — including action buttons. A background task subscribes to the
//! daemon's `ActionInvoked` signal and re-emits the portal's `ActionInvoked`
//! so the requesting app learns which button was pressed.

use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use futures::StreamExt;
use tracing::{debug, warn};
use zbus::{
    Connection, interface,
    object_server::SignalEmitter,
    proxy,
    zvariant::{OwnedValue, Value},
};

use crate::{dbus_util::opt_string, settings::PORTAL_PATH};

/// Minimal client for the freedesktop notification daemon.
#[proxy(
    interface = "org.freedesktop.Notifications",
    default_service = "org.freedesktop.Notifications",
    default_path = "/org/freedesktop/Notifications",
    gen_blocking = false
)]
trait Notifications {
    #[allow(clippy::too_many_arguments)]
    async fn notify(
        &self,
        app_name: &str,
        replaces_id: u32,
        app_icon: &str,
        summary: &str,
        body: &str,
        actions: Vec<&str>,
        hints: HashMap<&str, Value<'_>>,
        expire_timeout: i32,
    ) -> zbus::Result<u32>;

    async fn close_notification(&self, id: u32) -> zbus::Result<()>;

    /// Emitted by the daemon when the user activates an action.
    #[zbus(signal)]
    fn action_invoked(&self, id: u32, action_key: String) -> zbus::Result<()>;
}

/// What a displayed notification maps back to, keyed by daemon id.
#[derive(Clone)]
struct Tracked {
    app_id: String,
    portal_id: String,
}

/// Notification portal interface.
pub struct Notification {
    connection: Connection,
    /// daemon notification id -> the portal `(app_id, id)` that created it.
    tracked: Arc<Mutex<HashMap<u32, Tracked>>>,
}

impl Notification {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self {
            connection,
            tracked: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Spawns the task that forwards the daemon's `ActionInvoked` to the
    /// portal's `ActionInvoked`. Call once, before the object is mounted.
    pub fn spawn_action_forwarder(&self) {
        let connection = self.connection.clone();
        let tracked = self.tracked.clone();
        tokio::spawn(async move {
            let (proxy, emitter) = match (
                NotificationsProxy::new(&connection).await,
                SignalEmitter::new(&connection, PORTAL_PATH),
            ) {
                (Ok(proxy), Ok(emitter)) => (proxy, emitter),
                _ => {
                    warn!("notification: cannot subscribe to ActionInvoked");
                    return;
                }
            };
            let Ok(mut signals) = proxy.receive_action_invoked().await else {
                warn!("notification: ActionInvoked stream unavailable");
                return;
            };
            while let Some(signal) = signals.next().await {
                let Ok(args) = signal.args() else { continue };
                let Some(entry) = tracked.lock().ok().and_then(|m| m.get(&args.id).cloned()) else {
                    continue;
                };
                let result = Notification::action_invoked(
                    &emitter,
                    &entry.app_id,
                    &entry.portal_id,
                    args.action_key,
                    Vec::new(),
                )
                .await;
                if let Err(err) = result {
                    debug!(%err, "notification: failed to emit ActionInvoked");
                }
            }
        });
    }

    async fn proxy(&self) -> Option<NotificationsProxy<'_>> {
        match NotificationsProxy::new(&self.connection).await {
            Ok(proxy) => Some(proxy),
            Err(err) => {
                warn!(%err, "notification: daemon unavailable");
                None
            }
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Notification")]
impl Notification {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Displays a notification (with any action buttons).
    async fn add_notification(
        &self,
        app_id: String,
        id: String,
        notification: HashMap<String, OwnedValue>,
    ) {
        let summary = opt_string(&notification, "title").unwrap_or_else(|| app_id.clone());
        let body = opt_string(&notification, "body").unwrap_or_default();
        let icon = icon_name(&notification).unwrap_or_default();
        let actions = actions(&notification);
        let action_refs: Vec<&str> = actions.iter().map(String::as_str).collect();

        let Some(proxy) = self.proxy().await else {
            return;
        };
        match proxy
            .notify(&app_id, 0, &icon, &summary, &body, action_refs, HashMap::new(), -1)
            .await
        {
            Ok(daemon_id) => {
                if let Ok(mut map) = self.tracked.lock() {
                    map.insert(
                        daemon_id,
                        Tracked {
                            app_id,
                            portal_id: id,
                        },
                    );
                }
            }
            Err(err) => warn!(%err, "notification: Notify failed"),
        }
    }

    /// Closes a previously shown notification.
    async fn remove_notification(&self, app_id: String, id: String) {
        let daemon_id = self.tracked.lock().ok().and_then(|mut map| {
            let found = map
                .iter()
                .find(|(_, t)| t.app_id == app_id && t.portal_id == id)
                .map(|(daemon_id, _)| *daemon_id);
            if let Some(daemon_id) = found {
                map.remove(&daemon_id);
            }
            found
        });
        if let Some(daemon_id) = daemon_id
            && let Some(proxy) = self.proxy().await
            && let Err(err) = proxy.close_notification(daemon_id).await
        {
            warn!(%err, "notification: CloseNotification failed");
        }
    }

    /// Emitted when the user activates one of a notification's actions.
    #[zbus(signal)]
    async fn action_invoked(
        emitter: &SignalEmitter<'_>,
        app_id: &str,
        id: &str,
        action: String,
        parameter: Vec<OwnedValue>,
    ) -> zbus::Result<()>;
}

/// Builds the freedesktop `actions` array (`[key, label, …]`) from the portal
/// notification's `default-action` and `buttons`.
fn actions(notification: &HashMap<String, OwnedValue>) -> Vec<String> {
    let mut actions = Vec::new();
    if let Some(default) = opt_string(notification, "default-action") {
        actions.push(default);
        actions.push("Default".to_owned());
    }
    if let Some(value) = notification.get("buttons")
        && let Value::Array(array) = &**value
    {
        for item in array.iter() {
            if let Value::Dict(dict) = item {
                let label = dict_string(dict, "label");
                let action = dict_string(dict, "action");
                if let (Some(action), Some(label)) = (action, label) {
                    actions.push(action);
                    actions.push(label);
                }
            }
        }
    }
    actions
}

/// Reads a string entry from a zvariant dict.
fn dict_string(dict: &zbus::zvariant::Dict<'_, '_>, key: &str) -> Option<String> {
    dict.get::<&str, String>(&key).ok().flatten()
}

/// Best-effort icon name: portal icons are usually a `('themed', <as>)` or
/// `('file', <s>)` variant; we accept a bare string or the first themed name.
fn icon_name(notification: &HashMap<String, OwnedValue>) -> Option<String> {
    let value = notification.get("icon")?;
    if let Ok(name) = String::try_from(value.try_clone().ok()?) {
        return Some(name);
    }
    if let Value::Structure(structure) = &**value {
        for field in structure.fields() {
            if let Value::Array(array) = field {
                for item in array.iter() {
                    if let Value::Str(name) = item {
                        return Some(name.to_string());
                    }
                }
            }
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn icon_name_reads_bare_string() {
        let mut map = HashMap::new();
        map.insert(
            "icon".to_owned(),
            OwnedValue::try_from(Value::from("dialog-information")).unwrap(),
        );
        assert_eq!(icon_name(&map).as_deref(), Some("dialog-information"));
    }

    #[test]
    fn actions_includes_default_first() {
        let mut map = HashMap::new();
        map.insert(
            "default-action".to_owned(),
            OwnedValue::try_from(Value::from("open")).unwrap(),
        );
        let actions = actions(&map);
        assert_eq!(actions, vec!["open".to_owned(), "Default".to_owned()]);
    }

    #[test]
    fn actions_empty_without_buttons_or_default() {
        assert!(actions(&HashMap::new()).is_empty());
    }
}
