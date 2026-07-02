//! Desktop notifications via the session-bus `org.freedesktop.Notifications`
//! daemon (wayle runs its own).
//!
//! Sends over D-Bus directly rather than shelling out to `notify-send` — the
//! external binary is an optional dependency that may be absent from the
//! session's PATH, and its absence silently drops every notification.

use std::collections::HashMap;

use tracing::warn;
use zbus::{Connection, proxy, zvariant::Value};

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
}

/// Fire-and-forget desktop notification. Errors are logged, never returned — a
/// missing notification must not fail the caller's real work. `icon` is a
/// themed icon name or an absolute file path.
///
/// Spawns onto the tokio runtime the shell entered on the main thread, so it is
/// callable from both tokio and GTK/glib contexts.
pub fn notify(app_name: &str, summary: &str, body: &str, icon: &str) {
    let (app_name, summary, body, icon) = (
        app_name.to_owned(),
        summary.to_owned(),
        body.to_owned(),
        icon.to_owned(),
    );
    // ponytail: fresh session connection per call — notifications are rare, so
    // not worth threading a shared Connection through every call site.
    tokio::spawn(async move {
        let connection = match Connection::session().await {
            Ok(connection) => connection,
            Err(err) => return warn!(%err, "notify: no session bus"),
        };
        let proxy = match NotificationsProxy::new(&connection).await {
            Ok(proxy) => proxy,
            Err(err) => return warn!(%err, "notify: daemon unavailable"),
        };
        // Mirror libnotify: the icon also goes in the image-path hint, which is
        // what wayle's own popup consults in the default Automatic icon mode
        // (app_icon is only read in Application mode).
        let mut hints = HashMap::new();
        if !icon.is_empty() {
            hints.insert("image-path", Value::from(icon.as_str()));
        }
        if let Err(err) = proxy
            .notify(&app_name, 0, &icon, &summary, &body, Vec::new(), hints, -1)
            .await
        {
            warn!(%err, "notify: Notify failed");
        }
    });
}
