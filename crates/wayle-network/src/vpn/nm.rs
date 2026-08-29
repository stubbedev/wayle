//! NetworkManager backend: NM owns both the state and the control surface.
//!
//! Covers every profile NM manages — OpenVPN, WireGuard, OpenConnect, L2TP,
//! PPTP, Fortinet, and anything else with a VPN plugin — because the profile
//! type never has to be known here: a connection whose type is `vpn` or
//! `wireguard` is a VPN, and NM activates it the same way regardless.
//!
//! An entry's `id` is the connection's NM id (its name in `nmcli connection
//! show`), matched against the saved profiles rather than a D-Bus path, so the
//! config survives NM re-creating the profile.

use futures::StreamExt;
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};
use wayle_core::Property;
use zbus::{Connection, zvariant::OwnedObjectPath};

use super::VpnState;
use crate::{
    Error,
    proxy::{
        active_connection::ConnectionActiveProxy,
        manager::NetworkManagerProxy,
        settings::{SettingsProxy, connection::SettingsConnectionProxy},
    },
    types::{connectivity::ConnectionType, states::NMActiveConnectionState},
};

/// The root path, NM's "no specific object" placeholder.
fn root_path() -> OwnedObjectPath {
    OwnedObjectPath::try_from("/").unwrap_or_default()
}

/// Whether a connection type is a VPN as far as the indicator is concerned.
/// WireGuard is included: NM models it as its own type rather than as a VPN
/// plugin, but it is a tunnel and users expect it in the VPN list.
fn is_vpn_type(connection_type: &ConnectionType) -> bool {
    matches!(
        connection_type,
        ConnectionType::Vpn | ConnectionType::WireGuard
    )
}

/// The saved profile whose NM id is `id`.
async fn find_profile(connection: &Connection, id: &str) -> Result<Option<OwnedObjectPath>, Error> {
    let settings = SettingsProxy::new(connection).await?;
    for path in settings.list_connections().await? {
        let Ok(proxy) = SettingsConnectionProxy::new(connection, &path).await else {
            continue;
        };
        let Ok(config) = proxy.get_settings().await else {
            continue;
        };
        let Some(section) = config.get("connection") else {
            continue;
        };
        let profile_id = section
            .get("id")
            .and_then(|value| String::try_from(value.clone()).ok());
        let profile_type = section
            .get("type")
            .and_then(|value| String::try_from(value.clone()).ok())
            .map(|raw| ConnectionType::from_nm_type(&raw));
        if profile_id.as_deref() == Some(id) && profile_type.as_ref().is_some_and(is_vpn_type) {
            return Ok(Some(path));
        }
    }
    Ok(None)
}

/// The active connection currently running profile `id`, if any.
async fn find_active(connection: &Connection, id: &str) -> Result<Option<OwnedObjectPath>, Error> {
    let manager = NetworkManagerProxy::new(connection).await?;
    for path in manager.active_connections().await? {
        let Ok(proxy) = ConnectionActiveProxy::new(connection, &path).await else {
            continue;
        };
        if proxy.id().await.ok().as_deref() == Some(id) {
            return Ok(Some(path));
        }
    }
    Ok(None)
}

/// Activates the profile named `id`.
///
/// # Errors
///
/// Returns an error when no saved VPN profile carries that id, or when NM
/// refuses the activation.
pub(super) async fn activate(connection: &Connection, id: &str) -> Result<(), Error> {
    let Some(profile) = find_profile(connection, id).await? else {
        return Err(Error::ServiceInitializationFailed(format!(
            "no NetworkManager VPN profile named {id}"
        )));
    };
    let manager = NetworkManagerProxy::new(connection).await?;
    // Both device and specific_object are "/" — NM picks the base connection
    // for a VPN itself, which is what `nmcli connection up` does.
    manager
        .activate_connection(&profile, &root_path(), &root_path())
        .await?;
    Ok(())
}

/// Deactivates the active connection running profile `id`. A profile that is
/// already down is not an error.
///
/// # Errors
///
/// Returns an error when NM refuses the deactivation.
pub(super) async fn deactivate(connection: &Connection, id: &str) -> Result<(), Error> {
    let Some(active) = find_active(connection, id).await? else {
        return Ok(());
    };
    let manager = NetworkManagerProxy::new(connection).await?;
    manager.deactivate_connection(&active).await?;
    Ok(())
}

/// Maps NM's active-connection state to a VPN state.
fn state_of(state: NMActiveConnectionState) -> VpnState {
    match state {
        NMActiveConnectionState::Activated => VpnState::Connected,
        NMActiveConnectionState::Activating => VpnState::Connecting,
        _ => VpnState::Disconnected,
    }
}

/// Watches NM's active-connection list and drives `state` for profile `id`.
///
/// The list property is the subscription point rather than the VPN connection's
/// own signal: a VPN that is down has no active-connection object to watch at
/// all, so watching only the object would miss every connect.
pub(super) fn spawn_watcher(
    connection: Connection,
    id: String,
    state: Property<VpnState>,
    detail: Property<Option<String>>,
    token: CancellationToken,
) {
    tokio::spawn(async move {
        let Ok(manager) = NetworkManagerProxy::new(&connection).await else {
            warn!(vpn = %id, "cannot reach NetworkManager");
            state.set(VpnState::Failed);
            detail.set(Some(String::from("NetworkManager unavailable")));
            return;
        };

        let refresh = || async {
            match find_active(&connection, &id).await {
                Ok(Some(path)) => {
                    let raw = match ConnectionActiveProxy::new(&connection, &path).await {
                        Ok(proxy) => proxy.state().await.unwrap_or(0),
                        Err(_) => 0,
                    };
                    state_of(NMActiveConnectionState::from_u32(raw))
                }
                Ok(None) => VpnState::Disconnected,
                Err(error) => {
                    debug!(vpn = %id, %error, "cannot read active connections");
                    VpnState::Disconnected
                }
            }
        };

        let next = refresh().await;
        // Never clobber an in-flight attempt with "disconnected": NM has no
        // active connection yet while the profile is still being brought up.
        if !(next == VpnState::Disconnected && state.get().is_connecting()) {
            state.set(next);
        }

        let mut changes = manager.receive_active_connections_changed().await;
        loop {
            tokio::select! {
                () = token.cancelled() => break,
                change = changes.next() => {
                    if change.is_none() {
                        break;
                    }
                    let next = refresh().await;
                    if next == VpnState::Disconnected && state.get().is_connecting() {
                        continue;
                    }
                    state.set(next);
                }
            }
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn wireguard_counts_as_a_vpn() {
        assert!(is_vpn_type(&ConnectionType::Vpn));
        assert!(is_vpn_type(&ConnectionType::WireGuard));
        assert!(!is_vpn_type(&ConnectionType::Wifi));
        assert!(!is_vpn_type(&ConnectionType::Wired));
    }

    #[test]
    fn nm_states_map_to_vpn_states() {
        assert_eq!(
            state_of(NMActiveConnectionState::Activated),
            VpnState::Connected
        );
        assert_eq!(
            state_of(NMActiveConnectionState::Activating),
            VpnState::Connecting
        );
        assert_eq!(
            state_of(NMActiveConnectionState::Deactivated),
            VpnState::Disconnected
        );
    }
}
