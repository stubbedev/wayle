//! Watching the network a tunnel rides on.
//!
//! NetworkManager keeps a VPN `activated` for as long as its plugin process
//! runs, and openconnect keeps running while it retries a gateway it cannot
//! reach. So when the network under the tunnel loses its address — wifi
//! roaming to another access point restarts DHCP, and a slow router can leave
//! the machine without a lease for minutes — NM, and with it the row, still
//! says "connected" while nothing gets through. NM's own state does not move
//! either: the wifi device stays activated through a DHCP restart, and the
//! connectivity check that would notice runs minutes apart.
//!
//! So while a tunnel is up, the network under it is looked at directly: does
//! the connection carrying the traffic still hold an address? When it has
//! lost it for [`STALL_AFTER`] looks running, the tunnels that were up are
//! shown as reconnecting, with the reason. When the address is back, a tunnel
//! NM still runs is shown connected again (openconnect reconnects on its own
//! within its window), and one that dropped meanwhile — openconnect gave up —
//! is brought back the way a suspend brings it back, unless someone turned it
//! off in the meantime.

use std::{collections::HashMap, sync::Arc, time::Duration};

use tokio_util::sync::CancellationToken;
use tracing::{debug, info};
use wayle_core::Property;
use zbus::{Connection, zvariant::OwnedValue};

use super::{Vpn, VpnState, fold_states, nm, resume};
use crate::{
    proxy::{
        active_connection::ConnectionActiveProxy, ip4_config::IP4ConfigProxy,
        ip6_config::IP6ConfigProxy, manager::NetworkManagerProxy,
    },
    types::states::NMActiveConnectionState,
};

/// How often the network is looked at while a tunnel is up.
const LOOK_EVERY: Duration = Duration::from_secs(3);

/// Consecutive looks without an address before the tunnels count as stalled.
/// One miss is a DHCP renewal swapping its lease, which loses nothing.
const STALL_AFTER: u32 = 2;

/// The reason a stalled tunnel's row gives.
const WAITING: &str = "Waiting for the network";

/// What one look at the network changes.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Change {
    /// Nothing to do.
    None,
    /// The network has just been missing for [`STALL_AFTER`] looks.
    Lost,
    /// The network is back after having been lost.
    Back,
}

/// Folds one look into the run of looks without an address: the new run,
/// and what it changes.
fn next_look(misses: u32, usable: bool) -> (u32, Change) {
    if usable {
        let change = if misses >= STALL_AFTER {
            Change::Back
        } else {
            Change::None
        };
        return (0, change);
    }
    let misses = misses.saturating_add(1);
    let change = if misses == STALL_AFTER {
        Change::Lost
    } else {
        Change::None
    };
    (misses, change)
}

/// What becomes of a stalled tunnel once the network is back.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum AfterStall {
    /// NM still runs it: it is connected again (or getting there).
    Up,
    /// It dropped while the network was gone: bring it back.
    Restore,
    /// Someone turned it off meanwhile: it stays off.
    StayDown,
}

/// What to do with a stalled tunnel, given NM's state for it now (`None`
/// when NM no longer runs it) and whether someone turned it off.
fn after_stall(nm_state: Option<NMActiveConnectionState>, turned_off: bool) -> AfterStall {
    if turned_off {
        return AfterStall::StayDown;
    }
    match nm_state {
        Some(NMActiveConnectionState::Activated | NMActiveConnectionState::Activating) => {
            AfterStall::Up
        }
        _ => AfterStall::Restore,
    }
}

/// Whether an address read off NM's `AddressData` is one a tunnel can be
/// carried over: any IPv4 address, or an IPv6 one beyond the link.
fn is_routable(address: &str, ipv6: bool) -> bool {
    !address.is_empty() && !(ipv6 && address.to_ascii_lowercase().starts_with("fe80"))
}

/// Watches the network under the tunnels for as long as `token` is live.
pub(super) fn spawn(
    connection: Connection,
    entries: Property<Vec<Arc<Vpn>>>,
    aggregate: Property<VpnState>,
    token: CancellationToken,
) {
    tokio::spawn(async move {
        let mut misses = 0;
        loop {
            tokio::select! {
                () = token.cancelled() => break,
                () = tokio::time::sleep(LOOK_EVERY) => {}
            }

            let any_up_or_stalled = entries
                .get()
                .iter()
                .any(|vpn| vpn.state.get().is_connected() || vpn.stalled.get());
            if !any_up_or_stalled {
                misses = 0;
                continue;
            }

            let usable = network_usable(&connection).await;
            let (next, change) = next_look(misses, usable);
            misses = next;

            match change {
                Change::None if misses >= STALL_AFTER => stall(&entries, &aggregate),
                Change::None => {}
                Change::Lost => {
                    info!(
                        "the network under the VPN lost its address; showing the VPN as reconnecting"
                    );
                    stall(&entries, &aggregate);
                }
                Change::Back => {
                    info!("the network under the VPN is back");
                    recover(&connection, &entries, &aggregate, &token).await;
                }
            }
        }
    });
}

/// Shows every tunnel that is up as reconnecting. Said again on every look
/// while the network stays gone: NM's own watcher may have set a tunnel
/// connected since, from a state that is no longer true.
fn stall(entries: &Property<Vec<Arc<Vpn>>>, aggregate: &Property<VpnState>) {
    for vpn in entries.get() {
        if vpn.state.get().is_connected() || vpn.stalled.get() {
            vpn.stalled.set(true);
            if vpn.state.get() != VpnState::Connecting {
                vpn.state.set(VpnState::Connecting);
            }
            vpn.detail.set(Some(String::from(WAITING)));
        }
    }
    aggregate.set(fold_states(&entries.get()));
}

/// Settles every stalled tunnel once the network is back: connected again
/// when NM still runs it, restored when it dropped, left off when turned off.
async fn recover(
    connection: &Connection,
    entries: &Property<Vec<Arc<Vpn>>>,
    aggregate: &Property<VpnState>,
    token: &CancellationToken,
) {
    let running: HashMap<String, NMActiveConnectionState> = running_states(connection).await;
    let mut owed = Vec::new();

    for vpn in entries.get() {
        if !vpn.stalled.get() {
            continue;
        }
        vpn.stalled.set(false);
        vpn.detail.set(None);
        match after_stall(running.get(&vpn.uuid).copied(), vpn.turned_off.get()) {
            AfterStall::Up => {
                if let Some(path) = nm::active_by_uuid(connection)
                    .await
                    .ok()
                    .and_then(|active| active.get(&vpn.uuid).cloned())
                {
                    vpn.state.set(nm::state_at(connection, &path).await);
                }
            }
            AfterStall::Restore => {
                vpn.state.set(VpnState::Disconnected);
                owed.push(vpn.uuid.clone());
            }
            AfterStall::StayDown => vpn.state.set(VpnState::Disconnected),
        }
    }
    aggregate.set(fold_states(&entries.get()));

    if !owed.is_empty() {
        debug!(
            ?owed,
            "restoring the VPNs that dropped while the network was gone"
        );
        tokio::spawn(resume::restore(
            connection.clone(),
            entries.clone(),
            owed,
            token.child_token(),
        ));
    }
}

/// NM's state for each VPN it runs, by UUID.
async fn running_states(connection: &Connection) -> HashMap<String, NMActiveConnectionState> {
    let Ok(active) = nm::active_by_uuid(connection).await else {
        return HashMap::new();
    };
    let mut states = HashMap::with_capacity(active.len());
    for (uuid, path) in active {
        if let Ok(proxy) = ConnectionActiveProxy::new(connection, &path).await {
            states.insert(
                uuid,
                NMActiveConnectionState::from_u32(proxy.state().await.unwrap_or(0)),
            );
        }
    }
    states
}

/// Whether the connection carrying the traffic holds an address a tunnel can
/// ride: the primary connection, or — when a VPN carries the default route
/// itself — the connection under it. When NM cannot be asked, the network
/// counts as usable: a tunnel is never shown stalled on a guess.
async fn network_usable(connection: &Connection) -> bool {
    let Ok(manager) = NetworkManagerProxy::new(connection).await else {
        return true;
    };
    let Ok(primary) = manager.primary_connection().await else {
        return true;
    };
    if primary.as_str() == "/" {
        return false;
    }
    let Ok(mut active) = ConnectionActiveProxy::new(connection, &primary).await else {
        return true;
    };
    if active.vpn().await.unwrap_or(false) {
        let Ok(base) = active.specific_object().await else {
            return true;
        };
        if base.as_str() == "/" {
            return true;
        }
        let Ok(under) = ConnectionActiveProxy::new(connection, base).await else {
            return true;
        };
        active = under;
    }

    if let Ok(path) = active.ip4_config().await
        && path.as_str() != "/"
        && let Ok(config) = IP4ConfigProxy::new(connection, &path).await
        && has_routable(config.address_data().await.unwrap_or_default(), false)
    {
        return true;
    }
    if let Ok(path) = active.ip6_config().await
        && path.as_str() != "/"
        && let Ok(config) = IP6ConfigProxy::new(connection, &path).await
        && has_routable(config.address_data().await.unwrap_or_default(), true)
    {
        return true;
    }
    false
}

fn has_routable(data: Vec<HashMap<String, OwnedValue>>, ipv6: bool) -> bool {
    data.iter().any(|entry| {
        entry
            .get("address")
            .and_then(|value| <&str>::try_from(value).ok())
            .is_some_and(|address| is_routable(address, ipv6))
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_network_missing_for_the_stall_run_is_lost_once() {
        let (misses, change) = next_look(0, false);
        assert_eq!(
            (misses, change),
            (1, Change::None),
            "one miss is a lease swap"
        );

        let (misses, change) = next_look(misses, false);
        assert_eq!((misses, change), (STALL_AFTER, Change::Lost));

        let (misses, change) = next_look(misses, false);
        assert_eq!(change, Change::None, "lost is said once, not on every look");
        assert_eq!(misses, STALL_AFTER + 1);
    }

    #[test]
    fn a_network_back_after_being_lost_is_back_and_a_brief_miss_is_nothing() {
        assert_eq!(next_look(STALL_AFTER + 3, true), (0, Change::Back));
        assert_eq!(
            next_look(1, true),
            (0, Change::None),
            "a single miss never stalled anything, so nothing comes back"
        );
        assert_eq!(next_look(0, true), (0, Change::None));
    }

    #[test]
    fn a_stalled_tunnel_nm_still_runs_is_up_and_one_it_dropped_is_restored() {
        assert_eq!(
            after_stall(Some(NMActiveConnectionState::Activated), false),
            AfterStall::Up
        );
        assert_eq!(
            after_stall(Some(NMActiveConnectionState::Activating), false),
            AfterStall::Up
        );
        assert_eq!(after_stall(None, false), AfterStall::Restore);
        assert_eq!(
            after_stall(Some(NMActiveConnectionState::Deactivated), false),
            AfterStall::Restore
        );
    }

    #[test]
    fn a_stalled_tunnel_someone_turned_off_stays_off() {
        assert_eq!(after_stall(None, true), AfterStall::StayDown);
        assert_eq!(
            after_stall(Some(NMActiveConnectionState::Activated), true),
            AfterStall::StayDown
        );
    }

    #[test]
    fn any_ipv4_address_carries_a_tunnel_but_a_link_local_ipv6_one_does_not() {
        assert!(is_routable("192.168.1.166", false));
        assert!(is_routable("2a02:aa7:4000:1::5", true));
        assert!(!is_routable("fe80::1c2b:3cff:fe4d:5e6f", true));
        assert!(!is_routable("FE80::1", true));
        assert!(!is_routable("", false));
    }
}
