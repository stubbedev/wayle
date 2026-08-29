//! systemd backend: a unit is the source of truth for VPN *intent*.
//!
//! `ActiveState` gives the three states directly — `activating` is the
//! connecting window, `active` is connected, everything else is disconnected —
//! and `StartUnit`/`StopUnit` are the same polkit-gated calls `systemctl` makes,
//! so a setup already authorized for `systemctl start` needs no new policy.
//!
//! State arrives over `PropertiesChanged` on the unit object, which systemd
//! only emits for clients that have called `Subscribe()` first.

use futures::StreamExt;
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};
use wayle_core::Property;
use zbus::{Connection, proxy, zvariant::OwnedObjectPath};

use super::VpnState;
use crate::Error;

#[proxy(
    default_service = "org.freedesktop.systemd1",
    default_path = "/org/freedesktop/systemd1",
    interface = "org.freedesktop.systemd1.Manager"
)]
pub(crate) trait SystemdManager {
    /// Object path of a loaded unit; fails if the unit isn't loaded.
    fn get_unit(&self, name: &str) -> zbus::Result<OwnedObjectPath>;

    /// Object path of a unit, loading it if necessary.
    fn load_unit(&self, name: &str) -> zbus::Result<OwnedObjectPath>;

    /// Starts a unit. `mode` is the usual `replace`.
    fn start_unit(&self, name: &str, mode: &str) -> zbus::Result<OwnedObjectPath>;

    /// Stops a unit.
    fn stop_unit(&self, name: &str, mode: &str) -> zbus::Result<OwnedObjectPath>;

    /// Enables `PropertiesChanged` emission for unit objects. Without this
    /// systemd stays quiet and the watcher below never fires.
    fn subscribe(&self) -> zbus::Result<()>;
}

#[proxy(
    default_service = "org.freedesktop.systemd1",
    interface = "org.freedesktop.systemd1.Unit"
)]
pub(crate) trait SystemdUnit {
    /// `active` | `activating` | `deactivating` | `inactive` | `failed`.
    #[zbus(property)]
    fn active_state(&self) -> zbus::Result<String>;

    /// Finer-grained state, used as the failure detail.
    #[zbus(property)]
    fn sub_state(&self) -> zbus::Result<String>;
}

/// Starts `unit`.
///
/// # Errors
///
/// Propagates the D-Bus error, which is what a polkit denial arrives as.
pub(super) async fn start_unit(connection: &Connection, unit: &str) -> Result<(), Error> {
    let manager = SystemdManagerProxy::new(connection).await?;
    manager.start_unit(unit, "replace").await?;
    Ok(())
}

/// Stops `unit`.
///
/// # Errors
///
/// Propagates the D-Bus error.
pub(super) async fn stop_unit(connection: &Connection, unit: &str) -> Result<(), Error> {
    let manager = SystemdManagerProxy::new(connection).await?;
    manager.stop_unit(unit, "replace").await?;
    Ok(())
}

/// Maps `ActiveState` to a VPN state.
///
/// `deactivating` is disconnected rather than connecting: the tunnel is on its
/// way down, and showing a spinner for a teardown reads as a connect attempt.
fn state_of(active_state: &str) -> VpnState {
    match active_state {
        "active" => VpnState::Connected,
        "activating" | "reloading" => VpnState::Connecting,
        "failed" => VpnState::Failed,
        _ => VpnState::Disconnected,
    }
}

/// Watches `unit` and drives `state`.
///
/// When `link_owns_connected` is set the entry also has an `interface`, so the
/// link watcher decides `Connected` and this watcher stops at `Connecting` for
/// an active unit — the case where openconnect is alive but its tunnel is down
/// would otherwise read as connected.
pub(super) fn spawn_watcher(
    connection: Connection,
    unit: String,
    state: Property<VpnState>,
    detail: Property<Option<String>>,
    link_owns_connected: bool,
    token: CancellationToken,
) {
    tokio::spawn(async move {
        let apply = |active_state: &str, sub_state: Option<&str>| {
            let mut next = state_of(active_state);
            if link_owns_connected && next == VpnState::Connected {
                next = VpnState::Connecting;
            }
            state.set(next);
            detail.set(match next {
                VpnState::Failed => sub_state.map(ToOwned::to_owned),
                _ => None,
            });
        };

        let manager = match SystemdManagerProxy::new(&connection).await {
            Ok(manager) => manager,
            Err(error) => {
                warn!(%unit, %error, "cannot reach systemd");
                state.set(VpnState::Failed);
                detail.set(Some(String::from("systemd unavailable")));
                return;
            }
        };
        // Without Subscribe(), systemd emits no PropertiesChanged and this
        // watcher would report the initial state and then go silent forever.
        if let Err(error) = manager.subscribe().await {
            debug!(%unit, %error, "systemd Subscribe failed; continuing");
        }

        // LoadUnit rather than GetUnit: a VPN unit is normally inactive and
        // therefore not loaded, and GetUnit fails outright on those.
        let path = match manager.load_unit(&unit).await {
            Ok(path) => path,
            Err(error) => {
                warn!(%unit, %error, "cannot load unit");
                state.set(VpnState::Failed);
                detail.set(Some(format!("unknown unit {unit}")));
                return;
            }
        };
        let Ok(proxy) = SystemdUnitProxy::new(&connection, &path).await else {
            warn!(%unit, "cannot build unit proxy");
            return;
        };

        let active = proxy.active_state().await.unwrap_or_default();
        let sub = proxy.sub_state().await.unwrap_or_default();
        apply(&active, Some(sub.as_str()));

        let mut changes = proxy.receive_active_state_changed().await;
        loop {
            tokio::select! {
                () = token.cancelled() => break,
                next = changes.next() => {
                    let Some(change) = next else { break };
                    let Ok(active) = change.get().await else { continue };
                    let sub = proxy.sub_state().await.unwrap_or_default();
                    debug!(%unit, %active, %sub, "unit state changed");
                    apply(&active, Some(sub.as_str()));
                }
            }
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn active_state_maps_to_the_three_vpn_states() {
        assert_eq!(state_of("active"), VpnState::Connected);
        assert_eq!(state_of("activating"), VpnState::Connecting);
        assert_eq!(state_of("failed"), VpnState::Failed);
        assert_eq!(state_of("inactive"), VpnState::Disconnected);
        // A teardown is not a connect attempt.
        assert_eq!(state_of("deactivating"), VpnState::Disconnected);
    }
}
