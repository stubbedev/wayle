//! wayle as NetworkManager's secret agent.
//!
//! NM does not store every credential. A VPN password can be marked
//! not-saved, a 2FA challenge is by definition new each time, and an
//! OpenConnect session cookie is not a thing a user could type. For all of
//! those NM turns around and asks a registered *secret agent* — and if none is
//! registered, activation simply fails with "no secrets". That is the hole
//! that forced VPNs to be driven from outside wayle by helper scripts.
//!
//! Registering here closes it: NM asks wayle, wayle asks the user in the
//! network dropdown, and the answer goes straight back over the same call. No
//! nm-applet, no plugin auth-dialog binary, no shell.
//!
//! Coexistence is deliberate. Anything wayle cannot answer comes back as
//! `NoSecrets`, which is NM's cue to try the next agent rather than give up —
//! so running alongside nm-applet degrades to "whoever knows the answer wins".

mod fields;

use std::{
    collections::HashMap,
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    },
    time::{Duration, Instant},
};

use tokio::sync::{Mutex, oneshot};
use tokio_util::sync::CancellationToken;
use tracing::{debug, info, warn};
use wayle_core::Property;
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue, Value},
};

use crate::{
    Error,
    types::agent::{SecretReply, SecretRequest},
    vpn::openconnect,
};

/// Where NM expects the agent object to live. Not our choice: NM's agent
/// manager builds its proxy against this exact path.
const AGENT_PATH: &str = "/org/freedesktop/NetworkManager/SecretAgent";

/// Identifier we register under. Formatted like a D-Bus bus name, which is
/// what NM requires of it.
const AGENT_ID: &str = "com.wayle.network";

/// `NM_SECRET_AGENT_CAPABILITY_VPN_HINTS` — without it NM never passes the
/// per-key hints a VPN plugin asks with, and every VPN prompt would be a
/// guess at what the plugin actually wants.
const CAPABILITY_VPN_HINTS: u32 = 0x1;

/// `NM_SECRET_AGENT_GET_SECRETS_FLAG_ALLOW_INTERACTION`.
const ALLOW_INTERACTION: u32 = 0x1;

/// `NM_SECRET_AGENT_GET_SECRETS_FLAG_REQUEST_NEW` — NM is telling us the
/// stored secret was rejected, so asking the user again is the point.
const REQUEST_NEW: u32 = 0x2;

/// How long one request is worked on before it is given up.
///
/// NetworkManager waits 120 seconds for an agent's answer — a D-Bus timeout
/// fixed in `nm_secret_agent_get_secrets` — and then fails the activation for
/// want of secrets without telling the agent: no `CancelGetSecrets` follows a
/// timeout. Work that ran past it used to carry on regardless: a sign-in kept
/// waiting on a push nobody approved, and when it finally failed it wrote its
/// failure over the attempt that had come since and dropped the password for
/// it. This stops short of NM's limit so the answer, whatever it is, still
/// reaches someone.
const REQUEST_BUDGET: Duration = Duration::from_secs(110);

/// How long a restore's word that nobody is watching holds.
///
/// NM asks for an activation's secrets within moments of starting it. A mark
/// this old belongs to an activation that never got that far, and must not
/// turn some later one — started from `nmcli`, say — into one that refuses to
/// sign in.
const UNATTENDED_WINDOW: Duration = Duration::from_secs(30);

/// The errors NM understands from an agent. The names matter: `NoSecrets` is
/// what makes NM move on to the next agent instead of failing the activation.
#[derive(Debug, zbus::DBusError)]
#[zbus(prefix = "org.freedesktop.NetworkManager.SecretAgent.Error")]
#[allow(dead_code)]
pub(crate) enum SecretAgentError {
    /// Transport-level failure, mapped by zbus.
    #[zbus(error)]
    ZBus(zbus::Error),
    /// The agent has nothing to offer for this request.
    NoSecrets(String),
    /// The user dismissed the prompt.
    UserCanceled(String),
    /// The request was withdrawn before it was answered.
    AgentCanceled(String),
    /// The connection NM sent could not be understood.
    InvalidConnection(String),
    /// Anything else.
    InternalError(String),
}

/// A sign-in that did not produce secrets, and why.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthFailure {
    /// UUID of the profile that failed to authenticate.
    pub uuid: String,
    /// The reason, in the gateway's own words where it gave any.
    pub reason: String,
}

/// The pending-prompt state, shared between the D-Bus object and the UI.
#[derive(Debug)]
pub struct SecretAgentState {
    /// The prompt currently waiting on the user, if any.
    pub request: Property<Option<SecretRequest>>,
    /// The last sign-in failure, as `(profile uuid, reason)`.
    ///
    /// NM only ever reports "credentials not provided" for these, which tells
    /// the user nothing they can act on. The gateway's own wording — a wrong
    /// password, an expired account, a portal that wants SAML — is published
    /// here so the VPN row can show it instead.
    pub failure: Property<Option<AuthFailure>>,
    responder: std::sync::Mutex<Option<oneshot::Sender<SecretReply>>>,
    /// Serializes prompts. Two VPNs activating at once would otherwise race to
    /// own the single visible form, and the loser's request would hang until
    /// NM timed it out.
    prompt_lock: Mutex<()>,
    /// The requests being worked on, keyed the way `CancelGetSecrets` names
    /// them — connection path and setting — so a withdrawn request stops its
    /// own work and nobody else's.
    in_flight: std::sync::Mutex<HashMap<(String, String), (u64, CancellationToken)>>,
    next_request: AtomicU64,
    /// Profiles whose next activation has nobody watching it, and when that
    /// was said. See [`Self::expect_unattended`].
    unattended: std::sync::Mutex<HashMap<String, Instant>>,
}

impl SecretAgentState {
    pub(crate) fn new() -> Self {
        Self {
            request: Property::new(None),
            failure: Property::new(None),
            responder: std::sync::Mutex::new(None),
            prompt_lock: Mutex::new(()),
            in_flight: std::sync::Mutex::new(HashMap::new()),
            next_request: AtomicU64::new(0),
            unattended: std::sync::Mutex::new(HashMap::new()),
        }
    }

    /// Answers the pending prompt. A reply with no prompt waiting is dropped.
    pub async fn submit(&self, values: HashMap<String, String>) {
        self.answer(Some(values));
    }

    /// Dismisses the pending prompt, failing the activation NM was asking for.
    pub async fn cancel(&self) {
        self.answer(None);
    }

    fn answer(&self, reply: SecretReply) {
        let responder = self
            .responder
            .lock()
            .ok()
            .and_then(|mut responder| responder.take());
        self.request.set(None);
        if let Some(responder) = responder {
            let _ = responder.send(reply);
        }
    }

    /// Publishes a prompt and waits for the user, holding the prompt lock for
    /// the whole exchange so only one form is ever live.
    ///
    /// The wait can also end with no answer at all: the request it serves is
    /// withdrawn or runs out of time, and this future is dropped where it
    /// stands. The form goes with it. Left up, it would take an answer nobody
    /// is waiting for, and hold the lock every later prompt queues on.
    pub(crate) async fn prompt(&self, request: SecretRequest) -> SecretReply {
        let _turn = self.prompt_lock.lock().await;
        let (tx, rx) = oneshot::channel();
        if let Ok(mut responder) = self.responder.lock() {
            *responder = Some(tx);
        }
        self.request.set(Some(request));
        let _shown = Shown(self);

        rx.await.unwrap_or(None)
    }

    /// Says that the next activation of this profile is being made with nobody
    /// watching: a tunnel brought back after a suspend, or after NM restarted.
    ///
    /// Its sign-in may reuse a session that is still alive, and nothing else.
    /// Asking for a password, or posting one and so pushing a second factor to
    /// a phone nobody is looking at, is what made the first connect of the day
    /// hang: the push went out at wake, unseen, and the row sat on
    /// "connecting" until the user gave up on it and tried again.
    pub(crate) fn expect_unattended(&self, uuid: &str) {
        if let Ok(mut unattended) = self.unattended.lock() {
            unattended.insert(String::from(uuid), Instant::now());
        }
    }

    /// Takes back [`Self::expect_unattended`]: someone is here and asked.
    pub(crate) fn expect_attended(&self, uuid: &str) {
        if let Ok(mut unattended) = self.unattended.lock() {
            unattended.remove(uuid);
        }
    }

    /// Whether the request for this profile is the unattended one. The mark
    /// is used up either way: it speaks for one activation, not for every
    /// request after it.
    fn take_unattended(&self, uuid: &str, now: Instant) -> bool {
        self.unattended
            .lock()
            .ok()
            .and_then(|mut unattended| unattended.remove(uuid))
            .is_some_and(|at| now.saturating_duration_since(at) <= UNATTENDED_WINDOW)
    }

    /// Registers a request as in flight until the returned guard drops, and
    /// hands back the token its withdrawal cancels.
    ///
    /// A request for a connection and setting already in flight supersedes
    /// the older one: NM has stopped listening to that one, or it would not be
    /// asking again.
    fn begin(&self, path: &str, setting: &str) -> InFlight<'_> {
        let key = (String::from(path), String::from(setting));
        let id = self.next_request.fetch_add(1, Ordering::Relaxed);
        let token = CancellationToken::new();
        if let Ok(mut in_flight) = self.in_flight.lock()
            && let Some((_, older)) = in_flight.insert(key.clone(), (id, token.clone()))
        {
            older.cancel();
        }
        InFlight {
            state: self,
            key,
            id,
            token,
        }
    }

    /// Stops the request NM has withdrawn, wherever its work has got to.
    fn withdraw(&self, path: &str, setting: &str) {
        let key = (String::from(path), String::from(setting));
        if let Some((_, token)) = self
            .in_flight
            .lock()
            .ok()
            .and_then(|mut in_flight| in_flight.remove(&key))
        {
            token.cancel();
        }
    }
}

/// Takes a prompt down when the wait for its answer ends, however it ends.
struct Shown<'a>(&'a SecretAgentState);

impl Drop for Shown<'_> {
    fn drop(&mut self) {
        if let Ok(mut responder) = self.0.responder.lock() {
            responder.take();
        }
        self.0.request.set(None);
    }
}

/// One request NM is waiting on, for as long as it is being worked on.
struct InFlight<'a> {
    state: &'a SecretAgentState,
    key: (String, String),
    id: u64,
    token: CancellationToken,
}

impl Drop for InFlight<'_> {
    fn drop(&mut self) {
        // Only its own entry: a newer request under the same key has taken
        // the slot, and is not finished just because this one is.
        if let Ok(mut in_flight) = self.state.in_flight.lock()
            && in_flight
                .get(&self.key)
                .is_some_and(|(id, _)| *id == self.id)
        {
            in_flight.remove(&self.key);
        }
    }
}

/// The D-Bus object NM calls.
pub(crate) struct SecretAgent {
    state: Arc<SecretAgentState>,
    /// How long one request is worked on: [`REQUEST_BUDGET`], short of NM's
    /// own timeout.
    budget: Duration,
}

/// Deliberately outside the `#[interface]` block below: everything in *that*
/// one becomes a D-Bus method, and this is wayle's own.
impl SecretAgent {
    /// The answer to one request: a sign-in for an openconnect VPN, a prompt
    /// for everything else.
    async fn secrets_for(
        &self,
        connection: &HashMap<String, HashMap<String, OwnedValue>>,
        setting_name: &str,
        hints: &[String],
        flags: u32,
    ) -> Result<HashMap<String, HashMap<String, OwnedValue>>, SecretAgentError> {
        let profile = Profile::read(connection);
        let unattended = self.state.take_unattended(&profile.uuid, Instant::now());

        // An openconnect VPN's secrets are the result of a sign-in, not
        // something a person can type, so wayle performs the sign-in itself
        // rather than putting an un-fillable box on screen.
        if setting_name == "vpn"
            && let Some(vpn) = openconnect::profile(connection, &profile.uuid, &profile.id)
        {
            return self
                .vpn_secrets(&vpn, setting_name, flags, unattended)
                .await;
        }

        // A form nobody is there to fill in would sit on screen until the
        // budget ran out, and fail the activation just the same.
        if unattended {
            return Err(SecretAgentError::UserCanceled(String::from(
                "nobody was there to answer; connect again to sign in",
            )));
        }

        let Some(fields) = fields::for_request(setting_name, hints, &profile.connection_type)
        else {
            return Err(SecretAgentError::NoSecrets(format!(
                "nothing to ask for {setting_name}"
            )));
        };

        let request = SecretRequest {
            uuid: profile.uuid,
            name: profile.id,
            setting: String::from(setting_name),
            message: None,
            fields,
        };
        if flags & REQUEST_NEW != 0 {
            info!(name = %request.name, "previous credentials rejected, asking again");
        }

        match tokio::time::timeout(self.budget, self.state.prompt(request)).await {
            Ok(Some(values)) => Ok(reply_map(setting_name, values)),
            Ok(None) => Err(SecretAgentError::UserCanceled(String::from("dismissed"))),
            Err(_) => Err(SecretAgentError::UserCanceled(String::from(
                "no answer in time",
            ))),
        }
    }

    /// Signs in to an openconnect VPN and answers with the plugin's secrets.
    ///
    /// The three outcomes are three different things to tell NM, and mixing
    /// them up is what makes a VPN fail in a way nobody can act on:
    /// `NoSecrets` passes the request to the next agent, `UserCanceled` fails
    /// the activation with a reason the row can show, and a success clears
    /// whatever failure was on the row.
    async fn vpn_secrets(
        &self,
        vpn: &openconnect::Profile,
        setting_name: &str,
        flags: u32,
        unattended: bool,
    ) -> Result<HashMap<String, HashMap<String, OwnedValue>>, SecretAgentError> {
        if !openconnect::is_supported(vpn) {
            return Err(SecretAgentError::NoSecrets(format!(
                "no native sign-in for openconnect protocol {}",
                vpn.protocol
            )));
        }

        let signed_in = tokio::time::timeout(
            self.budget,
            openconnect::authenticate(vpn, flags & REQUEST_NEW != 0, unattended, &self.state),
        )
        .await
        .unwrap_or_else(|_| {
            Err(Error::VpnSignInIncomplete(String::from(
                "the sign-in did not finish in the time NetworkManager waits for one",
            )))
        });

        match signed_in {
            Ok(values) => {
                self.state.failure.set(None);
                Ok(reply_map(setting_name, values))
            }
            // A gateway wayle could not follow is not a failed sign-in: saying
            // NoSecrets hands the request on to the plugin's own auth dialog,
            // so claiming a protocol natively can never leave a VPN worse off
            // than it was before wayle claimed it.
            Err(error @ Error::VpnProtocolUnsupported(_)) => {
                warn!(name = %vpn.name, %error, "VPN sign-in not understood; leaving it to another agent");
                Err(SecretAgentError::NoSecrets(error.to_string()))
            }
            Err(error) => {
                warn!(name = %vpn.name, %error, "VPN sign-in failed");
                self.state.failure.set(Some(AuthFailure {
                    uuid: vpn.uuid.clone(),
                    reason: error.to_string(),
                }));
                Err(SecretAgentError::UserCanceled(error.to_string()))
            }
        }
    }
}

#[interface(name = "org.freedesktop.NetworkManager.SecretAgent")]
impl SecretAgent {
    /// NM needs credentials it does not have.
    async fn get_secrets(
        &self,
        connection: HashMap<String, HashMap<String, OwnedValue>>,
        connection_path: OwnedObjectPath,
        setting_name: String,
        hints: Vec<String>,
        flags: u32,
    ) -> Result<HashMap<String, HashMap<String, OwnedValue>>, SecretAgentError> {
        debug!(%connection_path, %setting_name, ?hints, flags, "secrets requested");

        // Without interaction there is nothing an interactive agent can add:
        // whatever NM already has is all there is. Saying so immediately lets
        // NM get on with retrying, rather than waiting on a prompt that is not
        // allowed to appear.
        if flags & ALLOW_INTERACTION == 0 {
            return Err(SecretAgentError::NoSecrets(String::from(
                "interaction not allowed",
            )));
        }

        // Withdrawn, the work is dropped wherever it has got to: a sign-in
        // stops waiting on the gateway, a prompt comes down, and nothing is
        // reported, because nobody is asking any more.
        let request = self.state.begin(connection_path.as_str(), &setting_name);
        tokio::select! {
            () = request.token.cancelled() => Err(SecretAgentError::AgentCanceled(
                String::from("withdrawn by NetworkManager"),
            )),
            secrets = self.secrets_for(&connection, &setting_name, &hints, flags) => secrets,
        }
    }

    /// NM gave up on a request — the activation was cancelled, or another
    /// agent answered first. That request stops, and only that one: another
    /// connection's prompt or sign-in is none of its business.
    fn cancel_get_secrets(&self, connection_path: OwnedObjectPath, setting_name: String) {
        debug!(%connection_path, %setting_name, "secret request withdrawn");
        self.state.withdraw(connection_path.as_str(), &setting_name);
    }

    /// NM asks agents to persist secrets they own. wayle owns none: everything
    /// it collects is handed straight back for NM's own store to keep.
    fn save_secrets(
        &self,
        _connection: HashMap<String, HashMap<String, OwnedValue>>,
        _connection_path: OwnedObjectPath,
    ) {
    }

    /// The mirror of [`Self::save_secrets`], and equally a no-op.
    fn delete_secrets(
        &self,
        _connection: HashMap<String, HashMap<String, OwnedValue>>,
        _connection_path: OwnedObjectPath,
    ) {
    }
}

/// The handful of fields worth pulling out of NM's connection dictionary.
struct Profile {
    id: String,
    uuid: String,
    connection_type: String,
}

impl Profile {
    fn read(connection: &HashMap<String, HashMap<String, OwnedValue>>) -> Self {
        let section = connection.get("connection");
        let get = |key: &str| {
            section
                .and_then(|section| section.get(key))
                .and_then(|value| String::try_from(value.clone()).ok())
                .unwrap_or_default()
        };
        let id = get("id");
        Self {
            uuid: get("uuid"),
            connection_type: get("type"),
            id,
        }
    }
}

/// Shapes the answer the way the setting expects.
///
/// A VPN's secrets are nested one level deeper than everyone else's — they go
/// in the `vpn` setting's own `secrets` sub-dictionary, not directly under the
/// setting. Getting this wrong reads to NM as "the agent returned nothing".
fn reply_map(
    setting_name: &str,
    values: HashMap<String, String>,
) -> HashMap<String, HashMap<String, OwnedValue>> {
    let mut setting: HashMap<String, OwnedValue> = HashMap::new();

    if setting_name == "vpn" {
        let nested: HashMap<String, String> = values;
        if let Ok(value) = OwnedValue::try_from(Value::from(nested)) {
            setting.insert(String::from("secrets"), value);
        }
    } else {
        for (key, value) in values {
            if let Ok(value) = OwnedValue::try_from(Value::from(value)) {
                setting.insert(key, value);
            }
        }
    }

    HashMap::from([(String::from(setting_name), setting)])
}

/// Serves the agent object and registers it with NetworkManager.
///
/// Returns the shared state so the service can expose the prompt and take the
/// user's answer.
///
/// # Errors
///
/// Returns an error when the object cannot be served. A failure to *register*
/// is logged rather than fatal: wayle still works without answering secrets,
/// and NM may simply not be up yet.
pub(crate) async fn serve(
    connection: &Connection,
    cancellation_token: CancellationToken,
) -> Result<Arc<SecretAgentState>, crate::Error> {
    let state = Arc::new(SecretAgentState::new());
    let agent = SecretAgent {
        state: Arc::clone(&state),
        budget: REQUEST_BUDGET,
    };

    connection
        .object_server()
        .at(AGENT_PATH, agent)
        .await
        .map_err(crate::Error::DbusError)?;

    register(connection).await;
    spawn_reregister(connection.clone(), cancellation_token);

    Ok(state)
}

async fn register(connection: &Connection) {
    use crate::proxy::agent_manager::AgentManagerProxy;

    let result = async {
        AgentManagerProxy::new(connection)
            .await?
            .register_with_capabilities(AGENT_ID, CAPABILITY_VPN_HINTS)
            .await
    }
    .await;

    match result {
        Ok(()) => info!("registered as NetworkManager secret agent"),
        Err(error) => warn!(%error, "cannot register as secret agent; VPN prompts will not appear"),
    }
}

/// Re-registers when NetworkManager comes back.
///
/// The registration lives in NM's process, so an NM restart silently drops it
/// — and a silently-unregistered agent looks exactly like a VPN that asks for
/// no credentials and then fails.
fn spawn_reregister(connection: Connection, cancellation_token: CancellationToken) {
    tokio::spawn(async move {
        let Ok(dbus) = zbus::fdo::DBusProxy::new(&connection).await else {
            return;
        };
        let Ok(mut changes) = dbus.receive_name_owner_changed().await else {
            return;
        };
        use futures::StreamExt;
        loop {
            tokio::select! {
                () = cancellation_token.cancelled() => break,
                next = changes.next() => {
                    let Some(signal) = next else { break };
                    let Ok(args) = signal.args() else { continue };
                    if args.name() != "org.freedesktop.NetworkManager" {
                        continue;
                    }
                    // Only a new owner is interesting; NM going away takes the
                    // registration with it and there is nothing to redo yet.
                    if args.new_owner().is_some() {
                        register(&connection).await;
                    }
                }
            }
        }
    });
}

#[cfg(test)]
// The VPN tests set `XDG_STATE_HOME`, so the cached password lives where the
// running shell keeps it.
#[allow(unsafe_code)]
mod tests {
    use super::*;
    use crate::vpn::openconnect::testing::{
        reached, remember_password, remembered_password, silent_gateway,
    };

    /// A connection, or an answer, in NM's `a{sa{sv}}` shape.
    type Settings = HashMap<String, HashMap<String, OwnedValue>>;

    fn value(text: &str) -> OwnedValue {
        OwnedValue::try_from(Value::from(text)).expect("a string value")
    }

    fn connection_section(uuid: &str, name: &str, kind: &str) -> HashMap<String, OwnedValue> {
        HashMap::from([
            (String::from("id"), value(name)),
            (String::from("uuid"), value(uuid)),
            (String::from("type"), value(kind)),
        ])
    }

    /// A GlobalProtect profile that has signed in before, as NM sends it.
    fn gp_connection(uuid: &str, gateway: &str) -> Settings {
        let data = HashMap::from([
            (String::from("gateway"), String::from(gateway)),
            (String::from("protocol"), String::from("gp")),
            (String::from("wayle-username"), String::from("alice")),
        ]);
        HashMap::from([
            (
                String::from("connection"),
                connection_section(uuid, "Work", "vpn"),
            ),
            (
                String::from("vpn"),
                HashMap::from([
                    (
                        String::from("service-type"),
                        value("org.freedesktop.NetworkManager.openconnect"),
                    ),
                    (
                        String::from("data"),
                        OwnedValue::try_from(Value::from(data)).expect("a dict value"),
                    ),
                ]),
            ),
        ])
    }

    /// A VPN wayle has no sign-in for, whose password is asked for in a form.
    fn form_vpn_connection(uuid: &str) -> Settings {
        HashMap::from([
            (
                String::from("connection"),
                connection_section(uuid, "Office", "vpn"),
            ),
            (
                String::from("vpn"),
                HashMap::from([(
                    String::from("service-type"),
                    value("org.freedesktop.NetworkManager.openvpn"),
                )]),
            ),
        ])
    }

    fn wifi_connection(uuid: &str) -> Settings {
        HashMap::from([(
            String::from("connection"),
            connection_section(uuid, "Home", "802-11-wireless"),
        )])
    }

    fn path(index: u32) -> OwnedObjectPath {
        OwnedObjectPath::try_from(format!("/org/freedesktop/NetworkManager/Settings/{index}"))
            .expect("an object path")
    }

    fn agent(budget: Duration) -> Arc<SecretAgent> {
        Arc::new(SecretAgent {
            state: Arc::new(SecretAgentState::new()),
            budget,
        })
    }

    fn state_home(label: &str) -> std::path::PathBuf {
        let base = std::env::temp_dir().join(format!("wayle-agent-{label}-{}", std::process::id()));
        // SAFETY: nextest runs every test in its own process, and nothing
        // else in this crate reads `XDG_STATE_HOME`.
        unsafe { std::env::set_var("XDG_STATE_HOME", &base) };
        base
    }

    /// Starts a request the way NM makes one: with interaction allowed.
    fn ask(
        agent: &Arc<SecretAgent>,
        connection: Settings,
        at: u32,
        setting: &str,
    ) -> tokio::task::JoinHandle<Result<Settings, SecretAgentError>> {
        let agent = Arc::clone(agent);
        let setting = String::from(setting);
        tokio::spawn(async move {
            agent
                .get_secrets(connection, path(at), setting, Vec::new(), ALLOW_INTERACTION)
                .await
        })
    }

    /// The prompt on screen, once one goes up.
    async fn prompt_shown(state: &SecretAgentState) -> Option<SecretRequest> {
        for _ in 0..100 {
            if let Some(request) = state.request.get() {
                return Some(request);
            }
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        None
    }

    fn psk(value: &str) -> HashMap<String, String> {
        HashMap::from([(String::from("psk"), String::from(value))])
    }

    #[tokio::test]
    async fn a_sign_in_networkmanager_withdraws_stops_where_it_is() {
        // The row clicked while it still says "connecting": NM cancels the
        // request. The sign-in used to carry on waiting on the gateway, and
        // its eventual failure landed on whatever attempt came next.
        let base = state_home("withdrawn");
        remember_password("withdrawn", "hunter2");
        let (gateway, accepted) = silent_gateway().await;
        let agent = agent(REQUEST_BUDGET);

        let request = ask(&agent, gp_connection("withdrawn", &gateway), 1, "vpn");
        assert!(reached(&accepted).await, "the sign-in never got going");

        agent.cancel_get_secrets(path(1), String::from("vpn"));
        let answer = tokio::time::timeout(Duration::from_secs(2), request)
            .await
            .expect("a withdrawn sign-in carried on waiting on the gateway")
            .expect("the request's task");
        assert!(
            matches!(answer, Err(SecretAgentError::AgentCanceled(_))),
            "got {answer:?}"
        );
        assert_eq!(
            agent.state.failure.get(),
            None,
            "a withdrawn sign-in reported a failure nobody was waiting for"
        );
        assert_eq!(
            remembered_password("withdrawn").as_deref(),
            Some("hunter2"),
            "a withdrawn sign-in cost the password"
        );

        let _ = std::fs::remove_dir_all(&base);
    }

    #[tokio::test]
    async fn withdrawing_one_request_leaves_the_others_alone() {
        let base = state_home("withdraw-one");
        remember_password("withdraw-one", "hunter2");
        let (gateway, accepted) = silent_gateway().await;
        let agent = agent(REQUEST_BUDGET);

        let vpn = ask(&agent, gp_connection("withdraw-one", &gateway), 1, "vpn");
        assert!(reached(&accepted).await, "the sign-in never got going");
        let wifi = ask(
            &agent,
            wifi_connection("home-wifi"),
            2,
            "802-11-wireless-security",
        );
        assert!(
            prompt_shown(&agent.state).await.is_some(),
            "the wifi prompt never went up"
        );

        agent.cancel_get_secrets(path(1), String::from("vpn"));
        let answer = tokio::time::timeout(Duration::from_secs(2), vpn)
            .await
            .expect("the withdrawn sign-in carried on")
            .expect("the request's task");
        assert!(
            matches!(answer, Err(SecretAgentError::AgentCanceled(_))),
            "got {answer:?}"
        );

        // Withdrawing the VPN used to dismiss whatever form was on screen.
        assert_eq!(
            agent.state.request.get().map(|request| request.uuid),
            Some(String::from("home-wifi")),
            "the wifi prompt went down with the VPN's request"
        );
        agent.state.submit(psk("correct horse")).await;
        let answer = tokio::time::timeout(Duration::from_secs(2), wifi)
            .await
            .expect("the wifi request never answered")
            .expect("the request's task");
        assert!(answer.is_ok(), "got {answer:?}");

        let _ = std::fs::remove_dir_all(&base);
    }

    #[tokio::test]
    async fn a_sign_in_that_outlasts_networkmanagers_patience_is_ended_and_says_why() {
        // NM stops listening after two minutes and says nothing. A sign-in
        // still waiting on a push past that point finished for nobody.
        let base = state_home("out-of-time");
        remember_password("out-of-time", "hunter2");
        let (gateway, _) = silent_gateway().await;
        let agent = agent(Duration::from_millis(300));

        let answer = tokio::time::timeout(
            Duration::from_secs(5),
            agent.get_secrets(
                gp_connection("out-of-time", &gateway),
                path(1),
                String::from("vpn"),
                Vec::new(),
                ALLOW_INTERACTION,
            ),
        )
        .await
        .expect("a sign-in ran on past the time NM waits for it");
        assert!(
            matches!(answer, Err(SecretAgentError::UserCanceled(_))),
            "got {answer:?}"
        );
        let failure = agent.state.failure.get().expect("the row is told why");
        assert_eq!(failure.uuid, "out-of-time");
        assert!(
            failure.reason.contains("did not finish"),
            "got: {}",
            failure.reason
        );
        assert_eq!(
            remembered_password("out-of-time").as_deref(),
            Some("hunter2"),
            "running out of time cost the password"
        );

        let _ = std::fs::remove_dir_all(&base);
    }

    #[tokio::test]
    async fn a_prompt_nobody_answers_comes_down_when_time_runs_out() {
        let agent = agent(Duration::from_millis(200));
        let answer = tokio::time::timeout(
            Duration::from_secs(5),
            agent.get_secrets(
                wifi_connection("home-wifi"),
                path(2),
                String::from("802-11-wireless-security"),
                Vec::new(),
                ALLOW_INTERACTION,
            ),
        )
        .await
        .expect("a prompt nobody answered waited forever");
        assert!(
            matches!(answer, Err(SecretAgentError::UserCanceled(_))),
            "got {answer:?}"
        );
        assert_eq!(
            agent.state.request.get(),
            None,
            "the form stayed up after NM stopped listening"
        );

        // Nor does the next prompt queue behind it, and an answer given in
        // time is still an answer.
        let patient = Arc::new(SecretAgent {
            state: Arc::clone(&agent.state),
            budget: REQUEST_BUDGET,
        });
        let next = ask(
            &patient,
            wifi_connection("office-wifi"),
            3,
            "802-11-wireless-security",
        );
        let shown = prompt_shown(&patient.state)
            .await
            .expect("the next prompt never went up");
        assert_eq!(shown.uuid, "office-wifi");
        patient.state.submit(psk("correct horse")).await;
        let answer = tokio::time::timeout(Duration::from_secs(2), next)
            .await
            .expect("the answered prompt never returned")
            .expect("the request's task");
        assert!(answer.is_ok(), "got {answer:?}");
    }

    #[test]
    fn a_restores_word_that_nobody_is_watching_holds_for_one_request() {
        let state = SecretAgentState::new();
        let now = Instant::now();
        state.expect_unattended("work");
        assert!(state.take_unattended("work", now));
        // Used up: a retry after it, or the next click, is someone asking.
        assert!(!state.take_unattended("work", now));
        assert!(!state.take_unattended("home", now), "an unmarked profile");
    }

    #[test]
    fn a_click_or_a_mark_gone_stale_is_attended() {
        let state = SecretAgentState::new();
        state.expect_unattended("work");
        state.expect_attended("work");
        assert!(
            !state.take_unattended("work", Instant::now()),
            "a click stayed unattended"
        );

        state.expect_unattended("work");
        let later = Instant::now() + UNATTENDED_WINDOW + Duration::from_secs(1);
        assert!(
            !state.take_unattended("work", later),
            "a mark outlived the activation it was made for"
        );
    }

    #[tokio::test]
    async fn nobody_is_shown_a_form_for_an_activation_nobody_is_watching() {
        let agent = agent(REQUEST_BUDGET);
        agent.state.expect_unattended("office");

        let answer = tokio::time::timeout(
            Duration::from_secs(2),
            agent.get_secrets(
                form_vpn_connection("office"),
                path(4),
                String::from("vpn"),
                vec![String::from("password")],
                ALLOW_INTERACTION,
            ),
        )
        .await
        .expect("waited on a form nobody will fill in");
        assert!(
            matches!(answer, Err(SecretAgentError::UserCanceled(_))),
            "got {answer:?}"
        );
        assert_eq!(agent.state.request.get(), None);

        // Asked for by someone, the same profile gets its form.
        let request = ask(&agent, form_vpn_connection("office"), 4, "vpn");
        let shown = prompt_shown(&agent.state)
            .await
            .expect("an attended request got no form");
        assert_eq!(shown.uuid, "office");
        agent.state.cancel().await;
        let _ = request.await;
    }

    #[test]
    fn vpn_secrets_are_nested_under_the_settings_own_secrets_key() {
        let reply = reply_map(
            "vpn",
            HashMap::from([(String::from("password"), String::from("hunter2"))]),
        );
        let vpn = reply.get("vpn").expect("vpn setting present");
        assert!(vpn.contains_key("secrets"), "vpn secrets must be nested");
        assert!(
            !vpn.contains_key("password"),
            "a flat vpn password reads to NM as no secrets at all"
        );
    }

    #[test]
    fn other_settings_take_their_secrets_flat() {
        let reply = reply_map(
            "802-11-wireless-security",
            HashMap::from([(String::from("psk"), String::from("hunter2"))]),
        );
        let security = reply
            .get("802-11-wireless-security")
            .expect("setting present");
        assert_eq!(
            String::try_from(security.get("psk").expect("psk present").clone()).as_deref(),
            Ok("hunter2")
        );
    }

    #[test]
    fn a_profile_with_no_connection_section_still_reads() {
        let profile = Profile::read(&HashMap::new());
        assert!(profile.id.is_empty());
        assert!(profile.uuid.is_empty());
    }
}
