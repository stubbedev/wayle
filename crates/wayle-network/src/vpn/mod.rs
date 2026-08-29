//! VPN state and control, across the three places a VPN can actually live.
//!
//! NetworkManager only knows about the VPNs it owns. An openconnect tunnel run
//! as a systemd unit, a `wg-quick@wg0`, a `tailscaled` — all invisible to NM,
//! and all common. So a VPN entry here names *where its state comes from*
//! rather than which VPN software it is:
//!
//! - [`VpnBackend::Networkmanager`] — NM is both the state source and the
//!   control surface.
//! - [`VpnBackend::Systemd`] — the unit's `ActiveState` is the source of truth
//!   for intent. `activating` is a real connecting state, which is the one
//!   thing no interface can report: during 2FA there is no tunnel yet.
//! - [`VpnBackend::Link`] — the interface is the source of truth for reality. A
//!   VPN process can be alive while its tunnel is down (dropped session, resume
//!   from suspend), and only the link sees that.
//!
//! The last two combine on one entry: give a `systemd` entry an `interface` and
//! the unit supplies `connecting` while the link decides `connected`. That
//! combination is what a hand-written `inotify` + `ip monitor` watcher script
//! ends up doing, and it is why neither source alone is enough.
//!
//! Everything here is event-driven. The systemd backend rides
//! `PropertiesChanged` on the unit object; the link backend rides an
//! `RTNLGRP_LINK` netlink socket. Nothing polls.

mod link;
mod nm;
mod systemd;

use std::{sync::Arc, time::Duration};

use futures::StreamExt;
use tokio::sync::Mutex;
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};
use wayle_config::schemas::modules::{VpnBackend, VpnBus, VpnEntry};
use wayle_core::Property;
use zbus::Connection;

use crate::Error;

/// The state of one VPN, as shown in the bar and the dropdown.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum VpnState {
    /// Not connected, and nothing in flight.
    #[default]
    Disconnected,
    /// A connection attempt is in flight — the unit is `activating`, NM is
    /// still negotiating, or a connect helper is running.
    Connecting,
    /// Connected: the tunnel is up.
    Connected,
    /// The last attempt failed. Collapses to disconnected for the bar icon;
    /// the dropdown shows the reason.
    Failed,
}

impl VpnState {
    /// Whether this state counts as "a VPN is up" for the module icon.
    #[must_use]
    pub const fn is_connected(self) -> bool {
        matches!(self, Self::Connected)
    }

    /// Whether a connection attempt is in flight.
    #[must_use]
    pub const fn is_connecting(self) -> bool {
        matches!(self, Self::Connecting)
    }
}

/// One configured VPN, its live state, and its toggle.
#[derive(Debug)]
pub struct Vpn {
    /// The entry this was built from.
    pub entry: VpnEntry,
    /// Live state.
    pub state: Property<VpnState>,
    /// Why the last attempt failed, when it did.
    pub detail: Property<Option<String>>,
    connection: Option<Connection>,
    /// Serializes toggles so a double-click can't start and stop at once.
    toggle_lock: Mutex<()>,
    cancellation_token: CancellationToken,
}

impl Vpn {
    /// Display name: the entry's `label`, else its `id`.
    #[must_use]
    pub fn name(&self) -> &str {
        self.entry.display_name()
    }

    /// Connects, if not already connected or connecting.
    ///
    /// # Errors
    ///
    /// Returns an error when the backend refuses the request — polkit denial,
    /// an unknown unit, a missing NM profile, or a connect command that failed
    /// to spawn.
    pub async fn connect(&self) -> Result<(), Error> {
        let _guard = self.toggle_lock.lock().await;
        if self.state.get().is_connected() {
            return Ok(());
        }
        // The state goes to Connecting up front rather than waiting for the
        // backend to report it: a helper doing interactive 2FA can take tens of
        // seconds before anything observable happens, and the whole point of a
        // connecting state is to cover exactly that window.
        self.state.set(VpnState::Connecting);
        self.detail.set(None);
        self.arm_connect_timeout();

        if let Some(command) = &self.entry.connect {
            return run_command(command);
        }
        match self.entry.backend {
            VpnBackend::Networkmanager => {
                nm::activate(self.conn()?, &self.entry.id).await?;
            }
            VpnBackend::Systemd => {
                systemd::start_unit(self.conn()?, self.unit()?).await?;
            }
            VpnBackend::Link => {
                self.state.set(VpnState::Failed);
                self.detail
                    .set(Some(String::from("no connect command configured")));
            }
        }
        Ok(())
    }

    /// Disconnects.
    ///
    /// # Errors
    ///
    /// Returns an error when the backend refuses the request.
    pub async fn disconnect(&self) -> Result<(), Error> {
        let _guard = self.toggle_lock.lock().await;
        if let Some(command) = &self.entry.disconnect {
            return run_command(command);
        }
        match self.entry.backend {
            VpnBackend::Networkmanager => nm::deactivate(self.conn()?, &self.entry.id).await,
            VpnBackend::Systemd => systemd::stop_unit(self.conn()?, self.unit()?).await,
            VpnBackend::Link => {
                self.detail
                    .set(Some(String::from("no disconnect command configured")));
                Ok(())
            }
        }
    }

    /// Connects when disconnected, disconnects when connected, and cancels an
    /// in-flight attempt when connecting.
    ///
    /// # Errors
    ///
    /// Propagates whichever of [`Self::connect`] / [`Self::disconnect`] ran.
    pub async fn toggle(&self) -> Result<(), Error> {
        if self.state.get() == VpnState::Disconnected || self.state.get() == VpnState::Failed {
            self.connect().await
        } else {
            // Clicking during `connecting` means cancel, which is a disconnect:
            // it tears down whatever half-started tunnel exists.
            self.disconnect().await
        }
    }

    fn conn(&self) -> Result<&Connection, Error> {
        self.connection.as_ref().ok_or_else(|| {
            Error::ServiceInitializationFailed(format!(
                "vpn {}: no bus connection for its backend",
                self.entry.id
            ))
        })
    }

    fn unit(&self) -> Result<&str, Error> {
        self.entry.unit.as_deref().ok_or_else(|| {
            Error::ServiceInitializationFailed(format!(
                "vpn {}: backend = \"systemd\" needs a unit",
                self.entry.id
            ))
        })
    }

    /// Drops back out of `connecting` after `connect-timeout` seconds if
    /// nothing has come up. Without this a failed 2FA leaves the indicator
    /// spinning forever, since the backend has nothing more to report.
    fn arm_connect_timeout(&self) {
        let state = self.state.clone();
        let detail = self.detail.clone();
        let timeout = Duration::from_secs(self.entry.connect_timeout);
        let token = self.cancellation_token.child_token();
        tokio::spawn(async move {
            tokio::select! {
                () = token.cancelled() => {}
                () = tokio::time::sleep(timeout) => {
                    if state.get().is_connecting() {
                        state.set(VpnState::Failed);
                        detail.set(Some(String::from("connect timed out")));
                    }
                }
            }
        });
    }
}

impl Drop for Vpn {
    fn drop(&mut self) {
        self.cancellation_token.cancel();
    }
}

/// Every configured VPN, plus the aggregate the bar icon reads.
#[derive(Debug)]
pub struct VpnService {
    /// One handle per `[[modules.network.vpn]]` entry, in config order.
    pub entries: Vec<Arc<Vpn>>,
    /// The state the module icon shows: connected if any is connected,
    /// connecting if any is connecting, else disconnected.
    pub aggregate: Property<VpnState>,
    cancellation_token: CancellationToken,
}

impl VpnService {
    /// Builds the service from config and starts watching every entry.
    ///
    /// Entries whose backend can't be reached (no systemd bus, a bad unit name)
    /// are kept with a `Failed` state and a reason rather than dropped — a VPN
    /// silently missing from the dropdown is worse than one showing why.
    ///
    /// # Errors
    ///
    /// Never fails as a whole; per-entry failures are surfaced as state.
    pub async fn new(entries: Vec<VpnEntry>, nm_connection: Connection) -> Self {
        let token = CancellationToken::new();
        let mut built = Vec::with_capacity(entries.len());
        for entry in entries {
            built.push(Arc::new(
                Self::build_entry(entry, &nm_connection, &token).await,
            ));
        }
        let service = Self {
            entries: built,
            aggregate: Property::new(VpnState::Disconnected),
            cancellation_token: token,
        };
        service.spawn_aggregate();
        service
    }

    /// Whether any VPN is configured at all — drives `vpn-show = "auto"`.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    /// The entry with this id.
    #[must_use]
    pub fn get(&self, id: &str) -> Option<&Arc<Vpn>> {
        self.entries.iter().find(|vpn| vpn.entry.id == id)
    }

    async fn build_entry(
        entry: VpnEntry,
        nm_connection: &Connection,
        parent: &CancellationToken,
    ) -> Vpn {
        let token = parent.child_token();
        let state = Property::new(VpnState::Disconnected);
        let detail = Property::new(None);

        let connection = match entry.backend {
            VpnBackend::Networkmanager => Some(nm_connection.clone()),
            VpnBackend::Systemd => match entry.bus {
                VpnBus::System => Connection::system().await.ok(),
                VpnBus::User => Connection::session().await.ok(),
            },
            VpnBackend::Link => None,
        };

        if entry.backend != VpnBackend::Link && connection.is_none() {
            warn!(vpn = %entry.id, "no bus connection for VPN backend");
            state.set(VpnState::Failed);
            detail.set(Some(String::from("bus unavailable")));
        }

        // The unit drives intent (including `connecting`); the interface, when
        // one is configured, overrides `connected` with reality. Both watchers
        // run: whichever reports last wins for its own half of the state.
        if let (VpnBackend::Systemd, Some(conn), Some(unit)) =
            (entry.backend, connection.as_ref(), entry.unit.as_deref())
        {
            systemd::spawn_watcher(
                conn.clone(),
                unit.to_owned(),
                state.clone(),
                detail.clone(),
                entry.interface.is_some(),
                token.child_token(),
            );
        }
        // Always the NM (system) bus: NM is the event source here, which is a
        // different bus from a user unit's session bus.
        if let Some(interface) = entry.interface.clone() {
            link::spawn_watcher(
                nm_connection.clone(),
                interface,
                state.clone(),
                token.child_token(),
            );
        }
        if let (VpnBackend::Networkmanager, Some(conn)) = (entry.backend, connection.as_ref()) {
            nm::spawn_watcher(
                conn.clone(),
                entry.id.clone(),
                state.clone(),
                detail.clone(),
                token.child_token(),
            );
        }

        Vpn {
            entry,
            state,
            detail,
            connection,
            toggle_lock: Mutex::new(()),
            cancellation_token: token,
        }
    }

    /// Folds every entry's state into [`Self::aggregate`], recomputing whenever
    /// any of them changes.
    fn spawn_aggregate(&self) {
        let states: Vec<Property<VpnState>> =
            self.entries.iter().map(|vpn| vpn.state.clone()).collect();
        if states.is_empty() {
            return;
        }
        let aggregate = self.aggregate.clone();
        let token = self.cancellation_token.child_token();
        let mut stream = futures::stream::select_all(
            states
                .iter()
                .map(|state| Box::pin(state.watch()))
                .collect::<Vec<_>>(),
        );
        let snapshot = states.clone();
        aggregate.set(fold_states(&snapshot));
        tokio::spawn(async move {
            loop {
                tokio::select! {
                    () = token.cancelled() => break,
                    next = stream.next() => {
                        if next.is_none() {
                            break;
                        }
                        aggregate.set(fold_states(&snapshot));
                    }
                }
            }
        });
    }
}

impl Drop for VpnService {
    fn drop(&mut self) {
        self.cancellation_token.cancel();
    }
}

/// Connected beats connecting beats everything else, so one connected VPN lights
/// the indicator even while another is still negotiating.
fn fold_states(states: &[Property<VpnState>]) -> VpnState {
    let mut result = VpnState::Disconnected;
    for state in states {
        match state.get() {
            VpnState::Connected => return VpnState::Connected,
            VpnState::Connecting => result = VpnState::Connecting,
            VpnState::Failed if result == VpnState::Disconnected => result = VpnState::Failed,
            _ => {}
        }
    }
    result
}

/// Spawns a configured `connect`/`disconnect` command.
///
/// Detached and not awaited: a connect helper is interactive (2FA) and can run
/// for a minute, and the state comes from the unit/link watchers regardless of
/// when the helper exits.
fn run_command(command: &str) -> Result<(), Error> {
    debug!(%command, "spawning vpn command");
    std::process::Command::new("sh")
        .arg("-c")
        .arg(command)
        .stdin(std::process::Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|error| {
            Error::ServiceInitializationFailed(format!("vpn command `{command}` failed: {error}"))
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn props(states: &[VpnState]) -> Vec<Property<VpnState>> {
        states.iter().copied().map(Property::new).collect()
    }

    #[test]
    fn aggregate_prefers_connected_then_connecting() {
        assert_eq!(fold_states(&props(&[])), VpnState::Disconnected);
        assert_eq!(
            fold_states(&props(&[VpnState::Disconnected, VpnState::Connecting])),
            VpnState::Connecting
        );
        // One connected VPN lights the indicator even while another negotiates.
        assert_eq!(
            fold_states(&props(&[VpnState::Connecting, VpnState::Connected])),
            VpnState::Connected
        );
        // Failed is only reported when nothing better is happening.
        assert_eq!(fold_states(&props(&[VpnState::Failed])), VpnState::Failed);
        assert_eq!(
            fold_states(&props(&[VpnState::Failed, VpnState::Connecting])),
            VpnState::Connecting
        );
    }
}
