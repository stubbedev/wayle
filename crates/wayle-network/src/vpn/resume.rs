//! Bringing tunnels back after a suspend.
//!
//! NetworkManager tears every VPN down when the machine sleeps and brings
//! none of them back on wake: a VPN is never autoconnected on its own. So the
//! tunnels that were up on the way down are recorded, and re-activated once
//! the network they ride on is back — through [`Vpn::connect`], the same path
//! a click takes, so the secret agent hands NM the cached session and a
//! reconnect costs no second factor.
//!
//! Only tunnels that were up are restored. One the user had turned off stays
//! off, and nothing is recorded across a reboot: the record lives in memory,
//! so this can never become a VPN that dials itself unattended.

use std::{sync::Arc, time::Duration};

use futures::StreamExt;
use tokio_util::sync::CancellationToken;
use tracing::{debug, info, warn};
use wayle_core::Property;
use zbus::{Connection, proxy};

use super::{Vpn, nm};
use crate::{
    Error,
    proxy::{active_connection::ConnectionActiveProxy, manager::NetworkManagerProxy},
    types::states::{NMActiveConnectionState, NMState},
};

#[proxy(
    interface = "org.freedesktop.login1.Manager",
    default_service = "org.freedesktop.login1",
    default_path = "/org/freedesktop/login1"
)]
trait Login1Manager {
    /// `true` just before the machine sleeps, `false` once it has woken.
    #[zbus(signal)]
    fn prepare_for_sleep(&self, start: bool) -> zbus::Result<()>;
}

/// How long to wait for the network after a wake. Wifi reassociating and a
/// DHCP lease on top routinely take tens of seconds; a network that has not
/// come back in this long is not the one the tunnel was up on.
const NETWORK_WAIT: Duration = Duration::from_secs(90);

/// Activation attempts per tunnel. The first right after the network is back
/// can still be refused — NM has a default route but has not settled which
/// connection a VPN rides on ("could not find source connection").
const ATTEMPTS: usize = 5;

/// Pause between attempts.
const RETRY_DELAY: Duration = Duration::from_secs(3);

/// Watches logind's sleep signal for as long as `token` is live.
pub(super) fn spawn(
    connection: Connection,
    entries: Property<Vec<Arc<Vpn>>>,
    token: CancellationToken,
) {
    tokio::spawn(async move {
        let Ok(login) = Login1ManagerProxy::new(&connection).await else {
            warn!("cannot reach logind; VPNs will not be restored after a suspend");
            return;
        };
        let Ok(mut signals) = login.receive_prepare_for_sleep().await else {
            warn!("cannot watch for suspend; VPNs will not be restored after one");
            return;
        };

        let mut was_up = Vec::new();
        // A restore still waiting on the network when the machine sleeps
        // again belongs to the previous wake: cancel it rather than let it
        // dial into the next one.
        let mut restoring = token.child_token();
        while let Some(sleeping) = next_sleep_signal(&mut signals, &token).await {
            if sleeping {
                restoring.cancel();
                was_up = up_now(&connection, &entries).await;
                debug!(?was_up, "going to sleep; recorded the VPNs that were up");
                continue;
            }
            let tunnels = std::mem::take(&mut was_up);
            if tunnels.is_empty() {
                continue;
            }
            restoring = token.child_token();
            tokio::spawn(restore(
                connection.clone(),
                entries.clone(),
                tunnels,
                restoring.clone(),
            ));
        }
    });
}

/// The next `PrepareForSleep` value, or `None` once the watch should end.
async fn next_sleep_signal(
    signals: &mut PrepareForSleepStream,
    token: &CancellationToken,
) -> Option<bool> {
    loop {
        tokio::select! {
            () = token.cancelled() => return None,
            next = signals.next() => {
                let signal = next?;
                if let Ok(args) = signal.args() {
                    return Some(*args.start());
                }
            }
        }
    }
}

/// The known VPNs NM is running right now, read straight off NM.
///
/// Asked of NM rather than of the entries' own state, which follows NM a
/// signal behind: NM starts tearing tunnels down on the same sleep signal,
/// and the entries may already read "disconnected" for a tunnel NM has only
/// just begun to take down.
async fn up_now(connection: &Connection, entries: &Property<Vec<Arc<Vpn>>>) -> Vec<String> {
    let Ok(active) = nm::active_by_uuid(connection).await else {
        return Vec::new();
    };
    let mut states = Vec::with_capacity(active.len());
    for (uuid, path) in active {
        let Ok(proxy) = ConnectionActiveProxy::new(connection, &path).await else {
            continue;
        };
        let state = NMActiveConnectionState::from_u32(proxy.state().await.unwrap_or(0));
        states.push((uuid, state));
    }
    let known: Vec<String> = entries.get().iter().map(|vpn| vpn.uuid.clone()).collect();
    to_restore(&states, &known)
}

/// Which of NM's active connections to bring back: the VPNs among them that
/// were up or on their way up.
///
/// `Deactivating` counts: by the time the sleep signal is read NM may already
/// be taking the tunnel down *because* of it. Anything NM runs that is not a
/// VPN wayle lists — the wifi, the wired link — is NM's own to bring back.
fn to_restore(active: &[(String, NMActiveConnectionState)], known: &[String]) -> Vec<String> {
    active
        .iter()
        .filter(|(uuid, state)| {
            known.contains(uuid)
                && matches!(
                    state,
                    NMActiveConnectionState::Activating
                        | NMActiveConnectionState::Activated
                        | NMActiveConnectionState::Deactivating
                )
        })
        .map(|(uuid, _)| uuid.clone())
        .collect()
}

/// Waits for the network, then brings each recorded tunnel back.
async fn restore(
    connection: Connection,
    entries: Property<Vec<Arc<Vpn>>>,
    tunnels: Vec<String>,
    token: CancellationToken,
) {
    if !network_back(&connection, &token).await {
        return;
    }

    for uuid in tunnels {
        let Some(vpn) = entries.get().into_iter().find(|vpn| vpn.uuid == uuid) else {
            // Deleted while the machine slept: nothing to restore.
            continue;
        };
        let outcome = restore_one(&vpn, &token).await;
        if token.is_cancelled() {
            return;
        }
        report(&vpn, &outcome);
    }
}

fn report(vpn: &Vpn, outcome: &Result<(), Error>) {
    match outcome {
        Ok(()) => info!(name = %vpn.name.get(), "VPN restored after resume"),
        Err(error) => warn!(name = %vpn.name.get(), %error, "cannot restore VPN after resume"),
    }
}

/// Brings one tunnel back, retrying while NM is still settling. A cancelled
/// restore gives up quietly.
async fn restore_one(vpn: &Vpn, token: &CancellationToken) -> Result<(), Error> {
    let mut attempt = 1;
    loop {
        match vpn.connect().await {
            Err(_) if attempt < ATTEMPTS => attempt += 1,
            outcome => return outcome,
        }
        tokio::select! {
            () = token.cancelled() => return Ok(()),
            () = tokio::time::sleep(RETRY_DELAY) => {}
        }
    }
}

/// Whether NM reports a usable network within [`NETWORK_WAIT`].
async fn network_back(connection: &Connection, token: &CancellationToken) -> bool {
    let Ok(manager) = NetworkManagerProxy::new(connection).await else {
        return false;
    };
    // Subscribed before the first read, so a change landing between the two
    // is not missed.
    let mut changes = manager.receive_state_property_changed().await;
    let wait = async {
        if is_network_back(manager.state_property().await.unwrap_or(0)) {
            return true;
        }
        while let Some(change) = changes.next().await {
            if change.get().await.is_ok_and(is_network_back) {
                return true;
            }
        }
        false
    };
    let back = tokio::select! {
        () = token.cancelled() => return false,
        back = tokio::time::timeout(NETWORK_WAIT, wait) => back.unwrap_or(false),
    };
    if !back {
        warn!("the network did not come back after resume; not restoring VPNs");
    }
    back
}

/// Whether an NM state has a default route a tunnel can ride.
///
/// `ConnectedSite` counts: it means NM's connectivity check has not passed,
/// which a gateway reachable over the local route does not care about.
/// `ConnectedLocal` does not — there is no default route yet, and an
/// activation now is refused outright.
fn is_network_back(state: u32) -> bool {
    matches!(
        NMState::from_u32(state),
        NMState::ConnectedSite | NMState::ConnectedGlobal
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn active(pairs: &[(&str, NMActiveConnectionState)]) -> Vec<(String, NMActiveConnectionState)> {
        pairs
            .iter()
            .map(|(uuid, state)| (String::from(*uuid), *state))
            .collect()
    }

    #[test]
    fn a_tunnel_up_or_on_its_way_up_is_restored() {
        let known = vec![
            String::from("work"),
            String::from("home"),
            String::from("lab"),
        ];
        let restored = to_restore(
            &active(&[
                ("work", NMActiveConnectionState::Activated),
                ("home", NMActiveConnectionState::Activating),
                // Already being torn down by the very sleep being recorded.
                ("lab", NMActiveConnectionState::Deactivating),
            ]),
            &known,
        );
        assert_eq!(restored, ["work", "home", "lab"]);
    }

    #[test]
    fn a_tunnel_that_was_down_is_not_dialled_on_wake() {
        let known = vec![String::from("work"), String::from("home")];
        let restored = to_restore(
            &active(&[
                ("work", NMActiveConnectionState::Deactivated),
                ("home", NMActiveConnectionState::Unknown),
            ]),
            &known,
        );
        assert!(
            restored.is_empty(),
            "restored a tunnel that was down: {restored:?}"
        );
        // And one that was not active at all is not in NM's list to begin with.
        assert!(to_restore(&[], &known).is_empty());
    }

    #[test]
    fn connections_that_are_not_listed_vpns_are_left_to_nm() {
        // The wifi and the wired link are active too; NM brings those back
        // itself, and activating them from here would fight it.
        let restored = to_restore(
            &active(&[
                ("wifi", NMActiveConnectionState::Activated),
                ("work", NMActiveConnectionState::Activated),
            ]),
            &[String::from("work")],
        );
        assert_eq!(restored, ["work"]);
    }

    #[test]
    fn the_network_is_back_once_it_has_a_default_route() {
        assert!(is_network_back(NMState::ConnectedGlobal as u32));
        assert!(is_network_back(NMState::ConnectedSite as u32));
    }

    #[test]
    fn a_network_still_waking_is_not_back() {
        for state in [
            NMState::Unknown,
            NMState::Asleep,
            NMState::Disconnected,
            NMState::Disconnecting,
            NMState::Connecting,
            // What NM reported at the moment an activation was refused with
            // "could not find source connection".
            NMState::ConnectedLocal,
        ] {
            assert!(!is_network_back(state as u32), "{state:?} counted as back");
        }
    }
}
