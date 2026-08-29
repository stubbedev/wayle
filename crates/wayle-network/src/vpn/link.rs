//! link backend: the tunnel interface is the source of truth for VPN *reality*.
//!
//! A VPN process can be alive while its tunnel is down — a dropped session, a
//! resume from suspend — so a unit alone will happily report "connected" for a
//! tunnel that carries nothing. The kernel always knows, so the link is what
//! decides `Connected`.
//!
//! State is read from `/sys/class/net/<iface>/operstate`, which is the same
//! ground truth `ip link` prints. The *events* come from NetworkManager's
//! device list rather than from a netlink socket: a tunnel device appears and
//! disappears with the tunnel, so `DeviceAdded`/`DeviceRemoved` fire on exactly
//! the edges that matter, and a device that stays but loses its carrier fires
//! `StateChanged`. An `AF_NETLINK` socket would be the more direct source, but
//! it needs `unsafe` to build a `sockaddr_nl`, and this crate forbids unsafe —
//! NM is already a hard dependency of the network module, so nothing is lost by
//! borrowing its event stream and re-reading sysfs on each edge.

use std::path::Path;

use futures::StreamExt;
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};
use wayle_core::Property;
use zbus::Connection;

use super::VpnState;
use crate::proxy::manager::NetworkManagerProxy;

/// Whether `interface` exists and is carrying.
///
/// `unknown` counts as up: tunnel devices (tun, wireguard) commonly never set a
/// carrier state and sit at `unknown` for their whole life, so treating it as
/// down would report every working VPN as disconnected.
fn interface_up(interface: &str) -> bool {
    let path = Path::new("/sys/class/net")
        .join(interface)
        .join("operstate");
    std::fs::read_to_string(path).is_ok_and(|state| matches!(state.trim(), "up" | "unknown"))
}

/// Watches `interface` and drives the `Connected` half of `state`.
///
/// Only the connected/disconnected distinction is this watcher's to make: it
/// never clears a `Connecting` set by the unit backend or by a connect helper,
/// because during 2FA there is no interface yet and dropping out of connecting
/// then would be wrong.
pub(super) fn spawn_watcher(
    connection: Connection,
    interface: String,
    state: Property<VpnState>,
    token: CancellationToken,
) {
    tokio::spawn(async move {
        let apply = |up: bool| {
            if up {
                state.set(VpnState::Connected);
            } else if !state.get().is_connecting() {
                state.set(VpnState::Disconnected);
            }
        };
        apply(interface_up(&interface));

        let Ok(manager) = NetworkManagerProxy::new(&connection).await else {
            warn!(%interface, "cannot reach NetworkManager; VPN link state is static");
            return;
        };
        let (Ok(mut added), Ok(mut removed), Ok(mut changed)) = (
            manager.receive_device_added().await,
            manager.receive_device_removed().await,
            manager.receive_state_changed().await,
        ) else {
            warn!(%interface, "cannot subscribe to device events");
            return;
        };

        loop {
            // A tunnel device appears/disappears with the tunnel, and a device
            // that survives but loses its carrier moves NM's own state — so any
            // of the three is a reason to re-read sysfs, which is authoritative.
            let event = tokio::select! {
                () = token.cancelled() => break,
                next = added.next() => next.map(|_| ()),
                next = removed.next() => next.map(|_| ()),
                next = changed.next() => next.map(|_| ()),
            };
            if event.is_none() {
                break;
            }
            let up = interface_up(&interface);
            debug!(%interface, up, "link event");
            apply(up);
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_missing_interface_is_down() {
        assert!(!interface_up("wayle-nonexistent-iface"));
    }

    #[test]
    fn loopback_is_up() {
        // `lo` exists on every Linux system and is always up, so this pins the
        // sysfs path and the operstate parsing without needing a real tunnel.
        assert!(interface_up("lo"));
    }
}
