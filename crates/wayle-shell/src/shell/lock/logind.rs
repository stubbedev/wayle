//! logind integration for the lock screen.
//!
//! Subscribes to the systemd-logind session `Lock`/`Unlock` signals so that
//! `loginctl lock-session`, idle daemons, and `wayle lock` all drive the lock
//! component, and reports the lock state back via `SetLockedHint` so other
//! session tooling agrees about whether the screen is locked.

use futures::StreamExt;
use relm4::Sender;
use tracing::{debug, info, warn};
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

    /// Whether the session is currently locked, as last hinted via
    /// `SetLockedHint`. Unlike the method call, the property is kept by
    /// logind and so outlives the process that set it.
    #[zbus(property)]
    fn locked_hint(&self) -> zbus::Result<bool>;

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

/// Reads the session's `LockedHint` property; `None` when logind or the
/// session is unavailable.
async fn locked_hint() -> Option<bool> {
    let result = async {
        let connection = Connection::system().await?;
        let proxy = SessionProxy::new(&connection).await?;
        proxy.locked_hint().await
    }
    .await;

    match result {
        Ok(locked) => Some(locked),
        Err(err) => {
            debug!(error = %err, "lock: LockedHint probe unavailable (non-fatal)");
            None
        }
    }
}

/// Emits [`LockInput::Lock`] when the session was locked when this shell
/// started, so a restart re-acquires the lock its dead predecessor held.
/// Best-effort: without logind, or with the hint unset, startup proceeds
/// unlocked.
pub(crate) async fn relock_at_startup(input: Sender<LockInput>) {
    if should_relock(locked_hint().await) {
        info!("lock: session was locked at startup (logind LockedHint); re-acquiring");
        input.emit(LockInput::Lock);
    }
}

/// Whether a fresh shell should re-acquire the session lock from the
/// `LockedHint` probe. Only a confirmed `true` relocks: `false` means the
/// previous shell left the session unlocked, and `None` (probe unavailable)
/// fails soft — an unknown state must not lock the user out.
fn should_relock(hint: Option<bool>) -> bool {
    hint == Some(true)
}

#[cfg(test)]
mod tests {
    use super::should_relock;

    #[test]
    fn relocks_only_on_a_confirmed_locked_hint() {
        assert!(
            should_relock(Some(true)),
            "LockedHint set: the previous shell died holding the lock, re-acquire"
        );
        assert!(
            !should_relock(Some(false)),
            "LockedHint clear: the session was unlocked, a restart must not lock it"
        );
        assert!(
            !should_relock(None),
            "probe unavailable: fail soft, an unknown state must not lock the user out"
        );
    }
}
