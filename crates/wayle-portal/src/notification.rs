//! `org.freedesktop.impl.portal.Notification`.
//!
//! Translates portal notifications into `org.freedesktop.Notifications.Notify`
//! calls, which the running shell's notification daemon (wayle-notification)
//! displays. Keeps an id map so `RemoveNotification` can close the right one.

use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use tracing::warn;
use zbus::{
    Connection, interface, proxy,
    zvariant::{OwnedValue, Value},
};

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
}

/// Notification portal interface.
pub struct Notification {
    connection: Connection,
    /// Maps `app_id/id` to the daemon notification id for removal.
    ids: Arc<Mutex<HashMap<String, u32>>>,
}

impl Notification {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self {
            connection,
            ids: Arc::new(Mutex::new(HashMap::new())),
        }
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

    /// Displays a notification.
    async fn add_notification(
        &self,
        app_id: String,
        id: String,
        notification: HashMap<String, OwnedValue>,
    ) {
        let summary = string_field(&notification, "title").unwrap_or_else(|| app_id.clone());
        let body = string_field(&notification, "body").unwrap_or_default();
        let icon = icon_name(&notification).unwrap_or_default();

        let Some(proxy) = self.proxy().await else {
            return;
        };
        match proxy
            .notify(&app_id, 0, &icon, &summary, &body, Vec::new(), HashMap::new(), -1)
            .await
        {
            Ok(daemon_id) => {
                if let Ok(mut map) = self.ids.lock() {
                    map.insert(key(&app_id, &id), daemon_id);
                }
            }
            Err(err) => warn!(%err, "notification: Notify failed"),
        }
    }

    /// Closes a previously shown notification.
    async fn remove_notification(&self, app_id: String, id: String) {
        let daemon_id = self.ids.lock().ok().and_then(|mut map| map.remove(&key(&app_id, &id)));
        if let Some(daemon_id) = daemon_id
            && let Some(proxy) = self.proxy().await
            && let Err(err) = proxy.close_notification(daemon_id).await
        {
            warn!(%err, "notification: CloseNotification failed");
        }
    }
}

/// Map key for an `(app_id, id)` pair.
fn key(app_id: &str, id: &str) -> String {
    format!("{app_id}/{id}")
}

/// Extracts a plain string field from the notification vardict.
fn string_field(notification: &HashMap<String, OwnedValue>, field: &str) -> Option<String> {
    let value = notification.get(field)?;
    String::try_from(value.try_clone().ok()?).ok()
}

/// Best-effort icon name: portal icons are usually a `('themed', <as>)` or
/// `('file', <s>)` variant; we accept a bare string or the first themed name.
fn icon_name(notification: &HashMap<String, OwnedValue>) -> Option<String> {
    let value = notification.get("icon")?;
    if let Ok(name) = String::try_from(value.try_clone().ok()?) {
        return Some(name);
    }
    // ('themed', ['icon-name', ...]) — dig out the first name.
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
    fn key_format() {
        assert_eq!(key("org.app", "n1"), "org.app/n1");
    }

    #[test]
    fn string_field_reads_title() {
        let mut map = HashMap::new();
        map.insert(
            "title".to_owned(),
            OwnedValue::try_from(Value::from("Hello")).unwrap(),
        );
        assert_eq!(string_field(&map, "title").as_deref(), Some("Hello"));
        assert_eq!(string_field(&map, "missing"), None);
    }

    #[test]
    fn icon_name_reads_bare_string() {
        let mut map = HashMap::new();
        map.insert(
            "icon".to_owned(),
            OwnedValue::try_from(Value::from("dialog-information")).unwrap(),
        );
        assert_eq!(icon_name(&map).as_deref(), Some("dialog-information"));
    }
}
