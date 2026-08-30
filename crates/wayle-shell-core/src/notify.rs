//! Desktop notifications via the session-bus `org.freedesktop.Notifications`
//! daemon (wayle runs its own).
//!
//! Sends over D-Bus directly rather than shelling out to `notify-send` — the
//! external binary is an optional dependency that may be absent from the
//! session's PATH, and its absence silently drops every notification.

use std::{
    collections::HashMap,
    sync::{
        Arc,
        atomic::{AtomicU32, Ordering},
    },
};

use futures::StreamExt;
use tokio::sync::oneshot;
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

    async fn close_notification(&self, id: u32) -> zbus::Result<()>;

    #[zbus(signal)]
    async fn action_invoked(&self, id: u32, action_key: String) -> zbus::Result<()>;

    #[zbus(signal)]
    async fn notification_closed(&self, id: u32) -> zbus::Result<()>;
}

/// What [`send_notification`] handed to the daemon, plus the signal streams
/// needed to observe the user's response.
struct SentNotification {
    connection: Connection,
    id: u32,
    invoked: Option<ActionInvokedStream>,
    closed: Option<NotificationClosedStream>,
}

async fn send_notification(
    app_name: &str,
    summary: &str,
    body: &str,
    icon: &str,
    actions: Vec<String>,
    expire_timeout: i32,
) -> zbus::Result<SentNotification> {
    // ponytail: fresh session connection per call — notifications are rare, so
    // not worth threading a shared Connection through every call site.
    let connection = Connection::session().await?;
    let proxy = NotificationsProxy::new(&connection).await?;

    // Subscribe before sending: signals can't race the id assignment that
    // way. Non-matching ids (other notifications) are skipped by the watcher.
    let has_actions = !actions.is_empty();
    let invoked = if has_actions {
        Some(proxy.receive_action_invoked().await?)
    } else {
        None
    };
    let closed = if has_actions {
        Some(proxy.receive_notification_closed().await?)
    } else {
        None
    };

    // Mirror libnotify: the icon also goes in the image-path hint, which is
    // what wayle's own popup consults in the default Automatic icon mode
    // (app_icon is only read in Application mode).
    let mut hints = HashMap::new();
    if !icon.is_empty() {
        hints.insert("image-path", Value::from(icon));
    }

    let action_refs: Vec<&str> = actions.iter().map(String::as_str).collect();
    let id = proxy
        .notify(
            app_name,
            0,
            icon,
            summary,
            body,
            action_refs,
            hints,
            expire_timeout,
        )
        .await?;

    Ok(SentNotification {
        connection,
        id,
        invoked,
        closed,
    })
}

/// Handle to a notification sent with actions, reporting what the user chose.
pub struct NotifyReceipt {
    id: Arc<AtomicU32>,
    action_rx: Option<oneshot::Receiver<Option<String>>>,
}

impl NotifyReceipt {
    /// Waits until the user invokes one of the notification's actions and
    /// returns its key, or `None` if the notification was closed or expired
    /// without an action being chosen.
    pub async fn action(&mut self) -> Option<String> {
        match self.action_rx.as_mut() {
            Some(rx) => rx.await.ok().flatten(),
            None => None,
        }
    }

    /// Removes the notification (fire-and-forget, best effort). A no-op if the
    /// daemon hasn't confirmed the notification yet — this module is
    /// fire-and-forget, not a delivery guarantee.
    pub fn close(&self) {
        let id = self.id.load(Ordering::Relaxed);
        if id == 0 {
            return;
        }
        tokio::spawn(async move {
            let connection = match Connection::session().await {
                Ok(connection) => connection,
                Err(err) => return warn!(%err, "notify: no session bus"),
            };
            let Ok(proxy) = NotificationsProxy::new(&connection).await else {
                return;
            };
            if let Err(err) = proxy.close_notification(id).await {
                warn!(%err, "notify: CloseNotification failed");
            }
        });
    }
}

/// Fire-and-forget desktop notification. Errors are logged, never returned — a
/// missing notification must not fail the caller's real work. `icon` is a
/// themed icon name or an absolute file path.
///
/// Spawns onto the tokio runtime the shell entered on the main thread, so it is
/// callable from both tokio and GTK/glib contexts.
pub fn notify(app_name: &str, summary: &str, body: &str, icon: &str) {
    notify_with_actions(app_name, summary, body, icon, &[], -1);
}

/// Like [`notify`], but attaches buttons and reports what the user did via the
/// returned [`NotifyReceipt`]. `actions` is a slice of `(key, label)` pairs;
/// `expire_timeout` is milliseconds (`-1` = daemon default, `0` = sticky).
///
/// The receipt's [`action()`](NotifyReceipt::action) resolves as soon as the
/// user clicks a button, and with `None` once the notification is closed or
/// times out — so awaiting it never hangs past the notification's lifetime.
pub fn notify_with_actions(
    app_name: &str,
    summary: &str,
    body: &str,
    icon: &str,
    actions: &[(&str, &str)],
    expire_timeout: i32,
) -> NotifyReceipt {
    let (app_name, summary, body, icon) = (
        app_name.to_owned(),
        summary.to_owned(),
        body.to_owned(),
        icon.to_owned(),
    );
    let dbus_actions: Vec<String> = actions
        .iter()
        .flat_map(|(key, label)| [(*key).to_owned(), (*label).to_owned()])
        .collect();
    let has_actions = !dbus_actions.is_empty();
    let (action_tx, action_rx) = oneshot::channel();
    let id_slot = Arc::new(AtomicU32::new(0));
    let id_slot_task = Arc::clone(&id_slot);

    tokio::spawn(async move {
        let sent = match send_notification(
            &app_name,
            &summary,
            &body,
            &icon,
            dbus_actions,
            expire_timeout,
        )
        .await
        {
            Ok(sent) => sent,
            Err(err) => {
                warn!(%err, "notify: Notify failed");
                let _ = action_tx.send(None);
                return;
            }
        };
        id_slot_task.store(sent.id, Ordering::Relaxed);

        if !has_actions {
            return;
        }

        tokio::spawn(watch_for_action(sent, action_tx));
    });

    NotifyReceipt {
        id: id_slot,
        action_rx: Some(action_rx),
    }
}

/// Resolves the receipt the moment `id` is either invoked (with the action
/// key) or closed (with `None`). Signals for other notifications are skipped.
async fn watch_for_action(sent: SentNotification, action_tx: oneshot::Sender<Option<String>>) {
    // Keep the session connection alive for the streams, even though it isn't
    // otherwise used here — the daemon's signals arrive over it.
    let _connection = sent.connection;
    let SentNotification {
        id,
        mut invoked,
        mut closed,
        ..
    } = sent;

    let resolved = loop {
        tokio::select! {
            Some(signal) = async {
                match invoked.as_mut() {
                    Some(stream) => stream.next().await,
                    None => std::future::pending().await,
                }
            } => {
                if signal.args().is_ok_and(|args| args.id == id) {
                    break signal.args().ok().map(|args| args.action_key);
                }
            }
            Some(signal) = async {
                match closed.as_mut() {
                    Some(stream) => stream.next().await,
                    None => std::future::pending().await,
                }
            } => {
                if signal.args().is_ok_and(|args| args.id == id) {
                    break None;
                }
            }
            else => break None,
        }
    };

    let _ = action_tx.send(resolved);
}
