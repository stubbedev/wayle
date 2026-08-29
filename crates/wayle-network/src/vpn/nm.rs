//! The NetworkManager side of a VPN: activating a profile, tearing one down,
//! and turning NM's active-connection states into [`VpnState`].
//!
//! Profiles themselves are not listed here — they come from the live
//! [`Settings::connections`](crate::core::settings::Settings) list the service
//! already maintains, so there is no second sweep of the settings tree.

use std::collections::HashMap;

use zbus::{Connection, zvariant::OwnedObjectPath};

use super::VpnState;
use crate::{
    Error,
    proxy::{active_connection::ConnectionActiveProxy, manager::NetworkManagerProxy},
    types::{
        connectivity::ConnectionType,
        states::{NMActiveConnectionState, NMActiveConnectionStateReason},
    },
};

/// NM's "no specific object" placeholder.
pub(super) fn root_path() -> OwnedObjectPath {
    OwnedObjectPath::try_from("/").unwrap_or_default()
}

/// Whether a connection type belongs in the VPN list.
///
/// WireGuard is included: NM models it as its own connection type rather than
/// as a VPN plugin — it needs no plugin at all, the kernel carries it — but it
/// is a tunnel and users expect it here.
pub(super) fn is_vpn_type(connection_type: &ConnectionType) -> bool {
    matches!(
        connection_type,
        ConnectionType::Vpn | ConnectionType::WireGuard
    )
}

/// The active connection currently running the profile with this UUID.
///
/// Matched on UUID rather than id: the id is the user-facing name and can be
/// edited from the widget, the UUID cannot.
pub(super) async fn find_active(
    connection: &Connection,
    uuid: &str,
) -> Result<Option<OwnedObjectPath>, Error> {
    let manager = NetworkManagerProxy::new(connection).await?;
    for path in manager.active_connections().await? {
        let Ok(proxy) = ConnectionActiveProxy::new(connection, &path).await else {
            continue;
        };
        if proxy.uuid().await.ok().as_deref() == Some(uuid) {
            return Ok(Some(path));
        }
    }
    Ok(None)
}

/// Every active VPN-ish connection, keyed by profile UUID.
///
/// Used to resync the whole list in one pass whenever NM's active-connection
/// list changes, rather than asking per entry.
pub(super) async fn active_by_uuid(
    connection: &Connection,
) -> Result<HashMap<String, OwnedObjectPath>, Error> {
    let manager = NetworkManagerProxy::new(connection).await?;
    let mut active = HashMap::new();
    for path in manager.active_connections().await? {
        let Ok(proxy) = ConnectionActiveProxy::new(connection, &path).await else {
            continue;
        };
        if let Ok(uuid) = proxy.uuid().await {
            active.insert(uuid, path);
        }
    }
    Ok(active)
}

/// Activates a saved profile and hands back its active-connection object.
///
/// Both `device` and `specific_object` are `/`: NM picks the base connection a
/// VPN rides on itself, which is what `nmcli connection up` does.
///
/// # Errors
///
/// Returns an error when NM refuses the activation — a missing plugin, a
/// polkit denial, or a profile NM cannot make sense of.
pub(super) async fn activate(
    connection: &Connection,
    profile: &OwnedObjectPath,
) -> Result<OwnedObjectPath, Error> {
    let manager = NetworkManagerProxy::new(connection).await?;
    let active = manager
        .activate_connection(profile, &root_path(), &root_path())
        .await?;
    Ok(active)
}

/// Tears down whatever is running this profile. Already down is not an error.
///
/// # Errors
///
/// Returns an error when NM refuses the deactivation.
pub(super) async fn deactivate(connection: &Connection, uuid: &str) -> Result<(), Error> {
    let Some(active) = find_active(connection, uuid).await? else {
        return Ok(());
    };
    let manager = NetworkManagerProxy::new(connection).await?;
    manager.deactivate_connection(&active).await?;
    Ok(())
}

/// Reads one active connection's current state.
pub(super) async fn state_at(connection: &Connection, path: &OwnedObjectPath) -> VpnState {
    let Ok(proxy) = ConnectionActiveProxy::new(connection, path).await else {
        return VpnState::Disconnected;
    };
    state_of(NMActiveConnectionState::from_u32(
        proxy.state().await.unwrap_or(0),
    ))
}

/// Maps NM's active-connection state to a VPN state.
pub(super) const fn state_of(state: NMActiveConnectionState) -> VpnState {
    match state {
        NMActiveConnectionState::Activated => VpnState::Connected,
        NMActiveConnectionState::Activating => VpnState::Connecting,
        _ => VpnState::Disconnected,
    }
}

/// A short, human-readable reason for a state change, or `None` when the
/// reason carries no information worth putting in front of the user.
///
/// Only failures get text: a VPN the user themselves disconnected does not
/// need a caption explaining that they did so.
pub(super) fn reason_text(reason: u32) -> Option<String> {
    let text = match NMActiveConnectionStateReason::from_u32(reason) {
        NMActiveConnectionStateReason::NoSecrets => "credentials not provided",
        NMActiveConnectionStateReason::LoginFailed => "authentication failed",
        NMActiveConnectionStateReason::ConnectTimeout => "connection timed out",
        NMActiveConnectionStateReason::ServiceStartTimeout => "VPN service did not start in time",
        NMActiveConnectionStateReason::ServiceStartFailed => "VPN service failed to start",
        NMActiveConnectionStateReason::ServiceStopped => "VPN service stopped",
        NMActiveConnectionStateReason::IpConfigInvalid => "invalid IP configuration",
        NMActiveConnectionStateReason::DependencyFailed => "the connection it depends on failed",
        NMActiveConnectionStateReason::DeviceRealizeFailed => "cannot create the tunnel device",
        NMActiveConnectionStateReason::DeviceRemoved
        | NMActiveConnectionStateReason::DeviceDisconnected => "the underlying network went away",
        _ => return None,
    };
    Some(String::from(text))
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

    #[test]
    fn only_failures_get_a_reason_caption() {
        assert_eq!(
            reason_text(NMActiveConnectionStateReason::LoginFailed as u32).as_deref(),
            Some("authentication failed")
        );
        assert_eq!(
            reason_text(NMActiveConnectionStateReason::NoSecrets as u32).as_deref(),
            Some("credentials not provided")
        );
        // The user pulling the plug is not a failure to explain back to them.
        assert_eq!(
            reason_text(NMActiveConnectionStateReason::UserDisconnected as u32),
            None
        );
        assert_eq!(
            reason_text(NMActiveConnectionStateReason::None as u32),
            None
        );
    }
}
