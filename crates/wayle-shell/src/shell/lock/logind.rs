//! logind integration for the lock screen.
//!
//! Subscribes to the systemd-logind session `Lock`/`Unlock` signals so that
//! `loginctl lock-session`, idle daemons, and `wayle lock` all drive the lock
//! component, and reports the lock state back via `SetLockedHint` so other
//! session tooling agrees about whether the screen is locked.

use futures::StreamExt;
use relm4::Sender;
use tracing::{debug, warn};
use zbus::{Connection, proxy};

use super::LockInput;

/// Proxy for the current session on `org.freedesktop.login1`.
#[proxy(
    interface = "org.freedesktop.login1.Session",
    default_service = "org.freedesktop.login1",
    default_path = "/org/freedesktop/login1/session/auto",
    gen_blocking = false
)]
trait Session {
    /// Hints to logind whether the session is currently locked.
    fn set_locked_hint(&self, locked: bool) -> zbus::Result<()>;

    /// Emitted when the session should lock (e.g. `loginctl lock-session`).
    #[zbus(signal)]
    fn lock(&self) -> zbus::Result<()>;

    /// Emitted when the session should unlock.
    #[zbus(signal)]
    fn unlock(&self) -> zbus::Result<()>;
}

/// Listens for logind Lock/Unlock signals and forwards them to the component.
///
/// Runs until the signal streams end (e.g. the bus drops). Any failure is
/// logged and ends the listener gracefully; the lock screen still works via the
/// CLI/IPC bridge if logind is unavailable.
pub(crate) async fn listen(input: Sender<LockInput>) {
    match listen_inner(&input).await {
        Ok(()) => debug!("lock: logind signal listener stopped"),
        Err(err) => warn!(error = %err, "lock: logind listener unavailable; triggers disabled"),
    }
}

/// Subscribes to the logind signals and pumps them into the component until a
/// stream ends. Errors propagate to [`listen`] for a single logging site.
async fn listen_inner(input: &Sender<LockInput>) -> zbus::Result<()> {
    let connection = Connection::system().await?;
    let proxy = SessionProxy::new(&connection).await?;
    let mut lock_signals = proxy.receive_lock().await?;
    let mut unlock_signals = proxy.receive_unlock().await?;

    debug!("lock: listening for logind Lock/Unlock signals");
    loop {
        tokio::select! {
            signal = lock_signals.next() => match signal {
                Some(_) => input.emit(LockInput::Lock),
                None => return Ok(()),
            },
            signal = unlock_signals.next() => match signal {
                Some(_) => input.emit(LockInput::ForceUnlock),
                None => return Ok(()),
            },
        }
    }
}

/// Reports the lock state to logind via `SetLockedHint`. Best-effort.
pub(crate) async fn set_locked_hint(locked: bool) {
    let result = async {
        let connection = Connection::system().await?;
        let proxy = SessionProxy::new(&connection).await?;
        proxy.set_locked_hint(locked).await
    }
    .await;

    if let Err(err) = result {
        debug!(error = %err, locked, "lock: SetLockedHint failed (non-fatal)");
    }
}
