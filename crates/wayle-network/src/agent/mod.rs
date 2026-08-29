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

use std::{collections::HashMap, sync::Arc};

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
    responder: Mutex<Option<oneshot::Sender<SecretReply>>>,
    /// Serializes prompts. Two VPNs activating at once would otherwise race to
    /// own the single visible form, and the loser's request would hang until
    /// NM timed it out.
    prompt_lock: Mutex<()>,
}

impl SecretAgentState {
    pub(crate) fn new() -> Self {
        Self {
            request: Property::new(None),
            failure: Property::new(None),
            responder: Mutex::new(None),
            prompt_lock: Mutex::new(()),
        }
    }

    /// Answers the pending prompt. A reply with no prompt waiting is dropped.
    pub async fn submit(&self, values: HashMap<String, String>) {
        self.answer(Some(values)).await;
    }

    /// Dismisses the pending prompt, failing the activation NM was asking for.
    pub async fn cancel(&self) {
        self.answer(None).await;
    }

    async fn answer(&self, reply: SecretReply) {
        let responder = self.responder.lock().await.take();
        self.request.set(None);
        if let Some(responder) = responder {
            let _ = responder.send(reply);
        }
    }

    /// Publishes a prompt and waits for the user, holding the prompt lock for
    /// the whole exchange so only one form is ever live.
    pub(crate) async fn prompt(&self, request: SecretRequest) -> SecretReply {
        let _turn = self.prompt_lock.lock().await;
        let (tx, rx) = oneshot::channel();
        *self.responder.lock().await = Some(tx);
        self.request.set(Some(request));

        let reply = rx.await.unwrap_or(None);
        self.request.set(None);
        reply
    }
}

/// The D-Bus object NM calls.
pub(crate) struct SecretAgent {
    state: Arc<SecretAgentState>,
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

        let profile = Profile::read(&connection);

        // An openconnect VPN's secrets are the result of a sign-in, not
        // something a person can type, so wayle performs the sign-in itself
        // rather than putting an un-fillable box on screen.
        if setting_name == "vpn"
            && let Some(vpn) = openconnect::profile(&connection, &profile.uuid, &profile.id)
        {
            if !openconnect::is_supported(&vpn) {
                return Err(SecretAgentError::NoSecrets(format!(
                    "no native sign-in for openconnect protocol {}",
                    vpn.protocol
                )));
            }
            return match openconnect::authenticate(&vpn, flags & REQUEST_NEW != 0, &self.state)
                .await
            {
                Ok(values) => {
                    self.state.failure.set(None);
                    Ok(reply_map(&setting_name, values))
                }
                // A gateway wayle could not follow is not a failed sign-in:
                // saying NoSecrets hands the request on to the plugin's own
                // auth dialog, so claiming a protocol natively can never leave
                // a VPN worse off than it was before wayle claimed it.
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
            };
        }

        let Some(fields) = fields::for_request(&setting_name, &hints, &profile.connection_type)
        else {
            return Err(SecretAgentError::NoSecrets(format!(
                "nothing to ask for {setting_name}"
            )));
        };

        let request = SecretRequest {
            uuid: profile.uuid,
            name: profile.id,
            setting: setting_name.clone(),
            message: None,
            fields,
        };
        if flags & REQUEST_NEW != 0 {
            info!(name = %request.name, "previous credentials rejected, asking again");
        }

        let Some(values) = self.state.prompt(request).await else {
            return Err(SecretAgentError::UserCanceled(String::from("dismissed")));
        };
        Ok(reply_map(&setting_name, values))
    }

    /// NM gave up on a request — the activation was cancelled, or another
    /// agent answered first.
    async fn cancel_get_secrets(&self, connection_path: OwnedObjectPath, setting_name: String) {
        debug!(%connection_path, %setting_name, "secret request withdrawn");
        self.state.cancel().await;
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
mod tests {
    use super::*;

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
