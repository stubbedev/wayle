//! Bringing tunnels back after a suspend.
//!
//! NetworkManager tears every VPN down when the machine sleeps and brings
//! none of them back on wake: a VPN is never autoconnected on its own. So the
//! tunnels that were up on the way down are recorded, and re-activated once
//! the network they ride on is back — through [`Vpn::connect`], the same path
//! a click takes, so the secret agent hands NM the cached session and a
//! reconnect costs no second factor.
//!
//! A restart of NetworkManager — a package upgrade — is the same story: it
//! takes the tunnels down on its way out and brings none of them back. So
//! the tunnels NM was running when it went away are brought back once it
//! returns, by the same path.
//!
//! Only tunnels that were up are restored. One the user had turned off stays
//! off, and nothing is recorded across a reboot: the record lives in memory,
//! so this can never become a VPN that dials itself unattended.

use std::{
    sync::Arc,
    time::{Duration, Instant},
};

use futures::StreamExt;
use tokio::task::JoinHandle;
use tokio_util::sync::CancellationToken;
use tracing::{debug, info, warn};
use wayle_core::Property;
use zbus::{Connection, proxy};

use super::{Vpn, VpnState, fold_states, nm};
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

/// How long before NetworkManager goes away a tunnel may have dropped and
/// still count as taken down by NM stopping. Its stop detaches the tunnels
/// first — the detach hook waits up to five seconds for openconnect — and
/// NM then takes a few more to exit.
const NM_STOP_WINDOW: Duration = Duration::from_secs(20);

/// How long to give the secret agent to re-register with a NetworkManager
/// that has just come back. Both follow the same bus signal; a tunnel
/// activated before the agent is back gets no cached session to sign in
/// with, and NM fails it for want of secrets.
const AGENT_SETTLE: Duration = Duration::from_secs(2);

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
        let mut pending: Option<(Vec<String>, JoinHandle<()>)> = None;
        while let Some(sleeping) = next_sleep_signal(&mut signals, &token).await {
            if sleeping {
                restoring.cancel();
                // The tunnels a cancelled restore never got to are still owed
                // a restore: they were up before the first sleep, and NM
                // tore them down then, so they are not among the ones up now.
                // A lid bounced on wake sleeps again within the second, which
                // is exactly when a restore is still waiting on the network.
                let unfinished = pending
                    .take()
                    .filter(|(_, handle)| !handle.is_finished())
                    .map(|(tunnels, _)| tunnels)
                    .unwrap_or_default();
                was_up = carried_over(unfinished, up_now(&connection, &entries).await);
                debug!(?was_up, "going to sleep; recorded the VPNs that were up");
                continue;
            }
            let tunnels = std::mem::take(&mut was_up);
            if tunnels.is_empty() {
                continue;
            }
            restoring = token.child_token();
            let handle = tokio::spawn(restore(
                connection.clone(),
                entries.clone(),
                tunnels.clone(),
                restoring.clone(),
            ));
            pending = Some((tunnels, handle));
        }
    });
}

/// Watches NetworkManager's bus name for as long as `token` is live, and
/// brings back the tunnels it was running when it went away.
pub(super) fn spawn_nm_restart(
    connection: Connection,
    entries: Property<Vec<Arc<Vpn>>>,
    aggregate: Property<VpnState>,
    token: CancellationToken,
) {
    tokio::spawn(async move {
        let Ok(dbus) = zbus::fdo::DBusProxy::new(&connection).await else {
            warn!("cannot reach the bus; VPNs will not be restored after NetworkManager restarts");
            return;
        };
        let Ok(mut changes) = dbus.receive_name_owner_changed().await else {
            warn!("cannot watch NetworkManager; VPNs will not be restored after it restarts");
            return;
        };

        let mut owed = Vec::new();
        let mut restoring = token.child_token();
        loop {
            let signal = tokio::select! {
                () = token.cancelled() => break,
                next = changes.next() => match next {
                    Some(signal) => signal,
                    None => break,
                },
            };
            let Ok(args) = signal.args() else { continue };
            if args.name() != "org.freedesktop.NetworkManager" {
                continue;
            }

            if args.old_owner().is_some() {
                restoring.cancel();
                let rows: Vec<_> = entries
                    .get()
                    .iter()
                    .map(|vpn| Row {
                        uuid: vpn.uuid.clone(),
                        state: vpn.state.get(),
                        went_down_at: vpn.went_down_at.get(),
                        turned_off: vpn.turned_off.get(),
                    })
                    .collect();
                owed = owed_after_nm_stop(&rows, Instant::now());
                // Nothing runs a tunnel without NM, and the objects whose
                // signals would have said so went away with it.
                for vpn in entries.get() {
                    if matches!(vpn.state.get(), VpnState::Connected | VpnState::Connecting) {
                        vpn.state.set(VpnState::Disconnected);
                    }
                }
                aggregate.set(fold_states(&entries.get()));
                debug!(
                    ?owed,
                    "NetworkManager went away; recorded the VPNs it was running"
                );
            }

            if args.new_owner().is_some() {
                let tunnels = std::mem::take(&mut owed);
                if tunnels.is_empty() {
                    continue;
                }
                restoring = token.child_token();
                let (connection, entries, restoring) =
                    (connection.clone(), entries.clone(), restoring.clone());
                tokio::spawn(async move {
                    tokio::select! {
                        () = restoring.cancelled() => return,
                        () = tokio::time::sleep(AGENT_SETTLE) => {}
                    }
                    restore(connection, entries, tunnels, restoring).await;
                });
            }
        }
        restoring.cancel();
    });
}

/// One VPN as NetworkManager went away.
#[derive(Debug)]
struct Row {
    uuid: String,
    state: VpnState,
    went_down_at: Option<Instant>,
    turned_off: bool,
}

/// Which tunnels to bring back once NetworkManager returns.
///
/// One still up went away with NM. One that went down within
/// [`NM_STOP_WINDOW`] was taken down by NM stopping — unless someone turned
/// it off, however recently, which keeps it off.
fn owed_after_nm_stop(rows: &[Row], now: Instant) -> Vec<String> {
    rows.iter()
        .filter(|row| {
            let up = row.state.is_connected() || row.state.is_connecting();
            let dropped_just_now = row
                .went_down_at
                .is_some_and(|at| now.saturating_duration_since(at) <= NM_STOP_WINDOW);
            up || (dropped_just_now && !row.turned_off)
        })
        .map(|row| row.uuid.clone())
        .collect()
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

/// What to restore on the next wake: the tunnels an interrupted restore still
/// owed, then the ones up now, each once.
fn carried_over(unfinished: Vec<String>, up: Vec<String>) -> Vec<String> {
    let mut tunnels = unfinished;
    for uuid in up {
        if !tunnels.contains(&uuid) {
            tunnels.push(uuid);
        }
    }
    tunnels
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

    fn uuids(names: &[&str]) -> Vec<String> {
        names.iter().map(|name| String::from(*name)).collect()
    }

    #[test]
    fn a_restore_cut_short_by_another_sleep_is_carried_over() {
        // Up before the first sleep, torn down by it, and not yet restored
        // when the second sleep came: NM reports nothing up, and the tunnel
        // must still come back on the second wake.
        assert_eq!(carried_over(uuids(&["work"]), Vec::new()), ["work"]);
        assert_eq!(
            carried_over(uuids(&["work"]), uuids(&["home", "work"])),
            ["work", "home"],
            "a tunnel both owed and up is restored once"
        );
    }

    #[test]
    fn nothing_owed_records_only_what_is_up() {
        assert_eq!(carried_over(Vec::new(), uuids(&["home"])), ["home"]);
        assert!(
            carried_over(Vec::new(), Vec::new()).is_empty(),
            "dialled a tunnel nothing recorded"
        );
    }

    fn row(uuid: &str, state: VpnState, went_down_at: Option<Instant>, turned_off: bool) -> Row {
        Row {
            uuid: String::from(uuid),
            state,
            went_down_at,
            turned_off,
        }
    }

    fn ago(now: Instant, secs: u64) -> Option<Instant> {
        now.checked_sub(Duration::from_secs(secs))
    }

    #[test]
    fn tunnels_networkmanager_took_with_it_are_owed() {
        let now = Instant::now();
        let owed = owed_after_nm_stop(
            &[
                // Still up as far as anyone heard: NM went away under it.
                row("work", VpnState::Connected, None, false),
                row("home", VpnState::Connecting, None, false),
                // Detached by NM's stop a few seconds before it exited.
                row("lab", VpnState::Failed, ago(now, 4), false),
                // Same, but the active-connection list said so before the
                // tunnel's own failure did.
                row("dc", VpnState::Disconnected, ago(now, 3), false),
            ],
            now,
        );
        assert_eq!(owed, ["work", "home", "lab", "dc"]);
    }

    #[test]
    fn tunnels_down_for_another_reason_are_not_owed() {
        let now = Instant::now();
        let owed = owed_after_nm_stop(
            &[
                // Turned off by the user, just before the restart.
                row("work", VpnState::Disconnected, ago(now, 1), true),
                row("vpn2", VpnState::Failed, ago(now, 1), true),
                // Failed long before NM stopped: not NM's doing.
                row("home", VpnState::Failed, ago(now, 600), false),
                // Failed without ever having been up: a refused sign-in.
                row("lab", VpnState::Failed, None, false),
                row("idle", VpnState::Disconnected, None, false),
            ],
            now,
        );
        assert!(
            owed.is_empty(),
            "owed a tunnel NM did not take down: {owed:?}"
        );
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
