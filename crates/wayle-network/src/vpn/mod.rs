//! VPN state and control, with NetworkManager as the single source of truth.
//!
//! Every VPN is an NM connection profile — a plugin VPN (OpenVPN,
//! OpenConnect and its whole protocol family, IPsec, L2TP, PPTP, SSTP, …) or a
//! native WireGuard profile, which needs no plugin at all because the kernel
//! carries it. Nothing is declared in wayle's config: the list is whatever NM
//! holds, so a VPN added from the widget shows up with no restart and no TOML.
//!
//! Two watchers drive the whole thing, and neither polls:
//!
//! - the profile list rides [`Settings::connections`], which NM's
//!   `NewConnection` / `ConnectionRemoved` signals already keep current;
//! - state rides NM's active-connection list, plus one `StateChanged`
//!   subscription per *currently active* VPN, because a tunnel going from
//!   `activating` to `activated` never changes the list it is in.
//!
//! Credentials — passwords, 2FA challenges, session cookies — are not handled
//! here. NM asks a registered secret agent for those; see [`crate::agent`].

pub mod kinds;
mod nm;
pub(crate) mod openconnect;
pub mod profile;
pub mod wg_quick;

use std::sync::Arc;

use futures::StreamExt;
use tokio::sync::Mutex;
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};
use wayle_core::Property;
use zbus::{Connection, zvariant::OwnedObjectPath};

use crate::{
    Error,
    agent::SecretAgentState,
    core::{settings::Settings, settings_connection::ConnectionSettings},
    proxy::{active_connection::ConnectionActiveProxy, manager::NetworkManagerProxy},
    types::{connectivity::ConnectionType, states::NMActiveConnectionState},
};

/// The state of one VPN, as shown in the bar and the dropdown.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum VpnState {
    /// Not connected, and nothing in flight.
    #[default]
    Disconnected,
    /// A connection attempt is in flight — NM is negotiating, or the plugin is
    /// still waiting on credentials.
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

/// One NetworkManager VPN profile, its live state, and its toggle.
#[derive(Debug)]
pub struct Vpn {
    /// The profile's NM UUID. Stable across renames, which is why rows and
    /// active connections are matched on it rather than on the name.
    pub uuid: String,
    /// The profile's NM id — the name shown in the dropdown. Shares the
    /// underlying property with the settings model, so a rename lands here
    /// without a rebuild.
    pub name: Property<String>,
    /// `true` for a native WireGuard profile, `false` for a plugin VPN. The
    /// distinction matters when editing: WireGuard has its own setting block
    /// rather than a `vpn` one.
    pub wireguard: bool,
    /// Live state.
    pub state: Property<VpnState>,
    /// Why the last attempt failed, when it did.
    pub detail: Property<Option<String>>,
    /// The profile's Settings object path, which is what activation takes.
    path: OwnedObjectPath,
    connection: Connection,
    /// Serializes toggles so a double-click can't start and stop at once.
    toggle_lock: Mutex<()>,
}

/// Two handles are the same VPN when they point at the same NM profile object.
/// This is what lets a rebuild triggered by unrelated profile churn — a new
/// wifi network, say — leave the VPN list, and its watchers, untouched.
impl PartialEq for Vpn {
    fn eq(&self, other: &Self) -> bool {
        self.uuid == other.uuid && self.path == other.path
    }
}

impl Vpn {
    /// Brings the tunnel up, if it isn't already.
    ///
    /// The connecting state is read back off the active-connection object NM
    /// creates rather than guessed: NM has the object in `activating` before
    /// `ActivateConnection` returns, so there is no window to paper over and
    /// no timeout needed to unwind an optimistic guess.
    ///
    /// # Errors
    ///
    /// Returns an error when NM refuses the activation — a missing VPN plugin,
    /// a polkit denial, or a profile it cannot make sense of. The reason is
    /// also published on [`Self::detail`], because the row is where the user is
    /// looking when it fails.
    pub async fn connect(&self) -> Result<(), Error> {
        let _guard = self.toggle_lock.lock().await;
        if self.state.get().is_connected() {
            return Ok(());
        }
        self.detail.set(None);

        match nm::activate(&self.connection, &self.path).await {
            Ok(active) => {
                self.state
                    .set(nm::state_at(&self.connection, &active).await);
                Ok(())
            }
            Err(error) => {
                self.state.set(VpnState::Failed);
                self.detail.set(Some(error.to_string()));
                Err(error)
            }
        }
    }

    /// Tears the tunnel down. Already down is not an error.
    ///
    /// # Errors
    ///
    /// Returns an error when NM refuses the deactivation.
    pub async fn disconnect(&self) -> Result<(), Error> {
        let _guard = self.toggle_lock.lock().await;
        nm::deactivate(&self.connection, &self.uuid).await
    }

    /// Connects when down, disconnects when up, and cancels an in-flight
    /// attempt when connecting.
    ///
    /// # Errors
    ///
    /// Propagates whichever of [`Self::connect`] / [`Self::disconnect`] ran.
    pub async fn toggle(&self) -> Result<(), Error> {
        if matches!(self.state.get(), VpnState::Disconnected | VpnState::Failed) {
            self.connect().await
        } else {
            // Clicking during `connecting` means cancel, which is a disconnect:
            // it tears down whatever half-started tunnel exists.
            self.disconnect().await
        }
    }
}

/// Every VPN NetworkManager knows about, plus the aggregate the bar icon reads.
#[derive(Debug)]
pub struct VpnService {
    /// One handle per NM VPN profile, in NM's own order. Rebuilt whenever a
    /// profile is added or removed.
    pub entries: Property<Vec<Arc<Vpn>>>,
    /// The state the module icon shows: connected if any is connected,
    /// connecting if any is connecting, else disconnected.
    pub aggregate: Property<VpnState>,
    settings: Arc<Settings>,
    cancellation_token: CancellationToken,
}

impl VpnService {
    /// Builds the service from NM's current profiles and starts watching.
    #[must_use]
    pub fn new(
        settings: &Arc<Settings>,
        connection: Connection,
        agent: &Arc<SecretAgentState>,
    ) -> Self {
        let token = CancellationToken::new();
        let entries = Property::new(build_entries(settings, &connection, &[]));
        let aggregate = Property::new(fold_states(&entries.get()));

        let service = Self {
            entries,
            aggregate,
            settings: Arc::clone(settings),
            cancellation_token: token,
        };
        service.spawn_profile_watcher(settings, &connection);
        service.spawn_active_watcher(connection);
        service.spawn_failure_watcher(agent);
        service
    }

    /// Copies a sign-in failure onto the row it belongs to.
    ///
    /// This has to land before NM reports the activation failed, and it does:
    /// the agent publishes the moment it gives up, which is what makes NM give
    /// up. [`resolve_state`]'s generic reason then defers to it.
    fn spawn_failure_watcher(&self, agent: &Arc<SecretAgentState>) {
        let failures = agent.failure.clone();
        let entries = self.entries.clone();
        let token = self.cancellation_token.child_token();

        tokio::spawn(async move {
            let mut changes = failures.watch();
            loop {
                tokio::select! {
                    () = token.cancelled() => break,
                    next = changes.next() => {
                        let Some(change) = next else { break };
                        let Some(failure) = change else { continue };
                        if let Some(vpn) =
                            entries.get().iter().find(|vpn| vpn.uuid == failure.uuid)
                        {
                            vpn.state.set(VpnState::Failed);
                            vpn.detail.set(Some(failure.reason));
                        }
                    }
                }
            }
        });
    }

    /// Whether NM knows of any VPN at all — drives `vpn-show = "auto"`.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.entries.get().is_empty()
    }

    /// The entry with this NM UUID.
    #[must_use]
    pub fn get(&self, uuid: &str) -> Option<Arc<Vpn>> {
        self.entries.get().into_iter().find(|vpn| vpn.uuid == uuid)
    }

    /// Creates a VPN profile from a kind and a set of form values.
    ///
    /// The new profile appears in [`Self::entries`] on its own: NM announces
    /// it, the profile watcher rebuilds, and the dropdown redraws. Nothing
    /// here has to tell the UI anything.
    ///
    /// # Errors
    ///
    /// Returns an error when NM rejects the profile — a missing plugin, a
    /// value it cannot parse, or a polkit denial.
    pub async fn add(
        &self,
        kind: &str,
        name: &str,
        values: &std::collections::HashMap<String, String>,
    ) -> Result<(), Error> {
        let uuid = new_uuid();
        let settings = profile::build(kind, name, &uuid, values);
        self.settings.add_connection(settings).await?;
        Ok(())
    }

    /// Rewrites an existing profile in place, keeping its UUID so anything
    /// referring to it — including a cached session — still matches.
    ///
    /// # Errors
    ///
    /// Returns an error when the profile is gone, or when NM rejects the
    /// rewrite.
    pub async fn update(
        &self,
        uuid: &str,
        kind: &str,
        name: &str,
        values: &std::collections::HashMap<String, String>,
    ) -> Result<(), Error> {
        let profile = self.profile_for(uuid)?;
        profile
            .update(profile::build(kind, name, uuid, values))
            .await
    }

    /// Deletes a profile, and whatever wayle cached for it.
    ///
    /// The cached session cookie and password are dropped with it: NM's own
    /// store goes when the profile does, and leaving wayle's behind would keep
    /// a live credential on disk for a profile that no longer exists, under a
    /// UUID nothing will ever look up again.
    ///
    /// # Errors
    ///
    /// Returns an error when the profile is gone, or when NM refuses. Nothing
    /// is forgotten in that case: the profile is still there.
    pub async fn remove(&self, uuid: &str) -> Result<(), Error> {
        self.profile_for(uuid)?.delete().await?;
        openconnect::forget(uuid);
        Ok(())
    }

    /// The saved settings of a profile, for prefilling the edit form.
    ///
    /// # Errors
    ///
    /// Returns an error when the profile is gone or unreadable.
    pub async fn settings_of(&self, uuid: &str) -> Result<profile::ConnectionDict, Error> {
        self.profile_for(uuid)?.get_settings().await
    }

    fn profile_for(&self, uuid: &str) -> Result<ConnectionSettings, Error> {
        self.settings
            .connections
            .get()
            .into_iter()
            .find(|profile| profile.uuid.get() == uuid)
            .ok_or_else(|| {
                Error::ServiceInitializationFailed(format!("no VPN profile with uuid {uuid}"))
            })
    }

    /// Rebuilds the entry list whenever NM's profile list changes, carrying
    /// each surviving profile's state across so an add or remove elsewhere in
    /// the list doesn't blank out a connected tunnel's row.
    fn spawn_profile_watcher(&self, settings: &Arc<Settings>, connection: &Connection) {
        let settings = settings.clone();
        let connection = connection.clone();
        let entries = self.entries.clone();
        let aggregate = self.aggregate.clone();
        let token = self.cancellation_token.child_token();

        tokio::spawn(async move {
            let mut changes = settings.connections.watch();
            loop {
                tokio::select! {
                    () = token.cancelled() => break,
                    next = changes.next() => {
                        if next.is_none() {
                            break;
                        }
                        let rebuilt = build_entries(&settings, &connection, &entries.get());
                        entries.set(rebuilt);
                        aggregate.set(fold_states(&entries.get()));
                    }
                }
            }
        });
    }

    /// Keeps every entry's state in step with NM.
    ///
    /// The active-connection list is the subscription point rather than each
    /// VPN's own object: a VPN that is down has no object to watch at all, so
    /// watching only objects would miss every connect. Objects are then watched
    /// individually on top, because `activating` → `activated` happens without
    /// the list ever changing.
    fn spawn_active_watcher(&self, connection: Connection) {
        let entries = self.entries.clone();
        let aggregate = self.aggregate.clone();
        let token = self.cancellation_token.child_token();

        tokio::spawn(async move {
            let Ok(manager) = NetworkManagerProxy::new(&connection).await else {
                warn!("cannot reach NetworkManager; VPN state will not update");
                return;
            };
            let mut list_changes = manager.receive_active_connections_changed().await;
            let mut entry_changes = entries.watch();
            // Per-object watchers live under their own token so a resync can
            // drop the whole previous generation in one go.
            let mut generation = token.child_token();

            resync(&connection, &entries, &aggregate, &generation).await;
            loop {
                tokio::select! {
                    () = token.cancelled() => break,
                    next = list_changes.next() => {
                        if next.is_none() {
                            break;
                        }
                    }
                    next = entry_changes.next() => {
                        if next.is_none() {
                            break;
                        }
                    }
                }
                generation.cancel();
                generation = token.child_token();
                resync(&connection, &entries, &aggregate, &generation).await;
            }
            generation.cancel();
        });
    }
}

impl Drop for VpnService {
    fn drop(&mut self) {
        self.cancellation_token.cancel();
    }
}

/// Derives the entry list from NM's saved profiles, reusing the state of any
/// profile that was already in the list.
fn build_entries(
    settings: &Settings,
    connection: &Connection,
    previous: &[Arc<Vpn>],
) -> Vec<Arc<Vpn>> {
    settings
        .connections
        .get()
        .iter()
        .filter(|profile| nm::is_vpn_type(&profile.connection_type.get()))
        .map(|profile| {
            let uuid = profile.uuid.get();
            let carried = previous.iter().find(|vpn| vpn.uuid == uuid);
            Arc::new(Vpn {
                wireguard: profile.connection_type.get() == ConnectionType::WireGuard,
                // Cloned, not copied: this is the same property the settings
                // model writes, so a rename lands without another rebuild.
                name: profile.id.clone(),
                state: carried.map_or_else(
                    || Property::new(VpnState::Disconnected),
                    |vpn| vpn.state.clone(),
                ),
                detail: carried.map_or_else(|| Property::new(None), |vpn| vpn.detail.clone()),
                uuid,
                path: profile.object_path.clone(),
                connection: connection.clone(),
                toggle_lock: Mutex::new(()),
            })
        })
        .collect()
}

/// Reads every entry's state straight off NM, and subscribes to the ones that
/// are currently up so their transitions arrive without another sweep.
async fn resync(
    connection: &Connection,
    entries: &Property<Vec<Arc<Vpn>>>,
    aggregate: &Property<VpnState>,
    generation: &CancellationToken,
) {
    let active = match nm::active_by_uuid(connection).await {
        Ok(active) => active,
        Err(error) => {
            debug!(%error, "cannot read active connections");
            return;
        }
    };

    for vpn in entries.get() {
        match active.get(&vpn.uuid) {
            Some(path) => {
                vpn.state.set(nm::state_at(connection, path).await);
                spawn_state_watcher(
                    connection.clone(),
                    path.clone(),
                    Arc::clone(&vpn),
                    entries.clone(),
                    aggregate.clone(),
                    generation.child_token(),
                );
            }
            // A failed attempt keeps its state and its reason: NM has already
            // torn the object down by the time anyone reads the row, and
            // "disconnected" with no explanation is exactly what the user
            // needed the reason for.
            None if vpn.state.get() != VpnState::Failed => {
                vpn.state.set(VpnState::Disconnected);
                vpn.detail.set(None);
            }
            None => {}
        }
    }
    aggregate.set(fold_states(&entries.get()));
}

/// Follows one active connection's `StateChanged` signal.
fn spawn_state_watcher(
    connection: Connection,
    path: OwnedObjectPath,
    vpn: Arc<Vpn>,
    entries: Property<Vec<Arc<Vpn>>>,
    aggregate: Property<VpnState>,
    token: CancellationToken,
) {
    tokio::spawn(async move {
        let Ok(proxy) = ConnectionActiveProxy::new(&connection, &path).await else {
            return;
        };
        let Ok(mut changes) = proxy.receive_active_connection_state_changed().await else {
            return;
        };
        loop {
            tokio::select! {
                () = token.cancelled() => break,
                next = changes.next() => {
                    let Some(signal) = next else { break };
                    let Ok(args) = signal.args() else { continue };
                    let (state, detail) = resolve_state(args.state, args.reason);
                    vpn.state.set(state);
                    vpn.detail.set(merge_detail(vpn.detail.get(), detail));
                    aggregate.set(fold_states(&entries.get()));
                }
            }
        }
    });
}

/// Turns one `StateChanged(state, reason)` into the row's state and caption.
///
/// A reason only becomes a caption when the connection actually went down: NM
/// repeats the last reason on every transition, so reading it on the way *up*
/// would caption a healthy tunnel with the story of the previous failure.
fn resolve_state(state: u32, reason: u32) -> (VpnState, Option<String>) {
    let mapped = nm::state_of(NMActiveConnectionState::from_u32(state));
    if mapped == VpnState::Disconnected
        && let Some(text) = nm::reason_text(reason)
    {
        return (VpnState::Failed, Some(text));
    }
    (mapped, None)
}

/// A random RFC 4122 version-4 UUID, in the form NM stores.
///
/// NM would generate one itself for a profile that arrives without, but then
/// it is only learned by reading the profile back — and an edit form that has
/// to re-read what it just wrote to find out what it created is a race with
/// every other client on the bus.
fn new_uuid() -> String {
    use rand::Rng;

    let mut bytes = [0_u8; 16];
    rand::rng().fill(&mut bytes);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;

    let hex: String = bytes.iter().map(|byte| format!("{byte:02x}")).collect();
    format!(
        "{}-{}-{}-{}-{}",
        &hex[0..8],
        &hex[8..12],
        &hex[12..16],
        &hex[16..20],
        &hex[20..32]
    )
}

/// Decides which of two failure captions the row should carry.
///
/// NM's own reason is generic — "credentials not provided" for every refused
/// sign-in. Whatever is already on the row came from the gateway and says why,
/// so it wins. A state change carrying no reason at all clears the row, which
/// is what makes a successful reconnect drop the last failure's caption.
fn merge_detail(existing: Option<String>, incoming: Option<String>) -> Option<String> {
    match incoming {
        None => None,
        Some(reason) => existing.or(Some(reason)),
    }
}

/// Connected beats connecting beats everything else, so one connected VPN lights
/// the indicator even while another is still negotiating.
fn fold_states(entries: &[Arc<Vpn>]) -> VpnState {
    fold(entries.iter().map(|vpn| vpn.state.get()))
}

fn fold(states: impl IntoIterator<Item = VpnState>) -> VpnState {
    let mut result = VpnState::Disconnected;
    for state in states {
        match state {
            VpnState::Connected => return VpnState::Connected,
            VpnState::Connecting => result = VpnState::Connecting,
            VpnState::Failed if result == VpnState::Disconnected => result = VpnState::Failed,
            _ => {}
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::states::NMActiveConnectionStateReason as Reason;

    #[test]
    fn aggregate_prefers_connected_then_connecting() {
        assert_eq!(fold([]), VpnState::Disconnected);
        assert_eq!(
            fold([VpnState::Disconnected, VpnState::Connecting]),
            VpnState::Connecting
        );
        // One connected VPN lights the indicator even while another negotiates.
        assert_eq!(
            fold([VpnState::Connecting, VpnState::Connected]),
            VpnState::Connected
        );
        // Failed is only reported when nothing better is happening.
        assert_eq!(fold([VpnState::Failed]), VpnState::Failed);
        assert_eq!(
            fold([VpnState::Failed, VpnState::Connecting]),
            VpnState::Connecting
        );
    }

    #[test]
    fn a_generated_uuid_is_a_version_four_uuid() {
        let uuid = new_uuid();
        assert_eq!(uuid.len(), 36);
        assert_eq!(uuid.chars().filter(|c| *c == '-').count(), 4, "got {uuid}");
        assert_eq!(uuid.as_bytes()[14], b'4', "version nibble: {uuid}");
        assert!(
            matches!(uuid.as_bytes()[19], b'8' | b'9' | b'a' | b'b'),
            "variant nibble: {uuid}"
        );
        // Two calls colliding would silently overwrite someone's profile.
        assert_ne!(new_uuid(), new_uuid());
    }

    #[test]
    fn the_gateways_own_reason_outranks_networkmanagers_generic_one() {
        let specific = Some(String::from("Invalid username or password"));
        assert_eq!(
            merge_detail(
                specific.clone(),
                Some(String::from("credentials not provided"))
            ),
            specific
        );
    }

    #[test]
    fn a_generic_reason_is_used_when_there_is_nothing_better() {
        assert_eq!(
            merge_detail(None, Some(String::from("connection timed out"))),
            Some(String::from("connection timed out"))
        );
    }

    #[test]
    fn a_clean_state_change_clears_even_a_specific_caption() {
        // Otherwise a successful reconnect keeps showing the last failure.
        assert_eq!(
            merge_detail(Some(String::from("Invalid password")), None),
            None
        );
    }

    #[test]
    fn a_deactivation_with_a_cause_becomes_a_failure_with_a_caption() {
        let (state, detail) = resolve_state(
            NMActiveConnectionState::Deactivated as u32,
            Reason::LoginFailed as u32,
        );
        assert_eq!(state, VpnState::Failed);
        assert_eq!(detail.as_deref(), Some("authentication failed"));
    }

    #[test]
    fn a_user_disconnect_is_just_disconnected() {
        let (state, detail) = resolve_state(
            NMActiveConnectionState::Deactivated as u32,
            Reason::UserDisconnected as u32,
        );
        assert_eq!(state, VpnState::Disconnected);
        assert_eq!(detail, None);
    }

    #[test]
    fn a_stale_failure_reason_does_not_caption_a_tunnel_coming_up() {
        // NM repeats the previous reason on the activating transition.
        let (state, detail) = resolve_state(
            NMActiveConnectionState::Activating as u32,
            Reason::LoginFailed as u32,
        );
        assert_eq!(state, VpnState::Connecting);
        assert_eq!(detail, None);
    }
}
