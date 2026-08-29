//! Producing the openconnect plugin's secrets without an openconnect binary.
//!
//! NetworkManager's openconnect plugin does not ask for a password. It asks
//! for a `cookie`, a `gateway` and a `gwcert` — the *output* of an
//! authentication, not its input. Conventionally an agent gets those by
//! spawning the plugin's own `nm-openconnect-auth-dialog`, which drives
//! libopenconnect through the whole sign-in.
//!
//! wayle authenticates itself instead. For GlobalProtect that is a form POST
//! and, when the gateway asks, a second one carrying the 2FA answer — plain
//! HTTPS, no tunnel involved, which is exactly what `openconnect
//! --authenticate` does before handing the cookie to something else. The
//! resulting cookie is cached, so a reconnect costs no second factor.
//!
//! The tunnel itself is still NetworkManager's plugin. This module replaces
//! the sign-in, the helper script and the systemd unit around it — not the
//! thing that moves packets.

mod cache;
mod cert;
mod form;
mod gp;
mod xml;

use std::{
    collections::HashMap,
    sync::{LazyLock, Mutex},
    time::{Duration, Instant},
};

use tracing::{debug, info, warn};
use zbus::zvariant::OwnedValue;

use crate::{
    Error,
    agent::SecretAgentState,
    types::agent::{SecretField, SecretRequest},
};

/// The NM service name of the openconnect plugin.
const SERVICE_TYPE: &str = "org.freedesktop.NetworkManager.openconnect";

/// Where wayle keeps the sign-in username. The plugin has no key for it — its
/// auth dialog asks every time — so this is wayle's own, stored in the profile
/// where it is a setting the user can see and edit rather than hidden state.
const USERNAME_KEY: &str = "wayle-username";

/// How many challenge rounds to follow before giving up. A gateway that keeps
/// asking is misconfigured or hostile; either way an unbounded loop would keep
/// the prompt on screen forever.
const MAX_CHALLENGES: usize = 5;

/// What openconnect reports itself as. Some gateways gate on it.
const USER_AGENT: &str = "PAN GlobalProtect";

/// How long a request may take to reach the gateway at all. Short, because
/// nothing about it waits on a person.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(15);

/// How long a prelogin may take. It is a static document; a gateway that has
/// not answered in this long is not going to.
const PRELOGIN_TIMEOUT: Duration = Duration::from_secs(20);

/// How long the login post may take.
///
/// Generous on purpose: a gateway doing push MFA holds this request open until
/// the user has approved it on their phone, so the timeout that fits every
/// other request would fail exactly the person who is slow to find it.
const LOGIN_TIMEOUT: Duration = Duration::from_secs(180);

/// How long a cookie stays "just handed over" for. NM re-asks within
/// milliseconds when the plugin rejects a secret set, so anything on this
/// scale is a retry rather than a reconnect. See [`is_spent`].
const RETRY_WINDOW: Duration = Duration::from_secs(120);

/// The cookie last handed to NM per profile, and when.
///
/// NM does not set `REQUEST_NEW` on every retry — on a rejected secret set it
/// often does not set it at all — so without this the same stale cookie is
/// handed back on every attempt and the activation loops instead of failing.
static HANDED_OUT: LazyLock<Mutex<HashMap<String, (String, Instant)>>> =
    LazyLock::new(|| Mutex::new(HashMap::new()));

/// The openconnect settings wayle needs out of an NM profile.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct Profile {
    pub uuid: String,
    pub name: String,
    pub gateway: String,
    pub protocol: String,
    pub username: Option<String>,
}

/// Reads an openconnect profile out of NM's connection dictionary, or `None`
/// when this is some other kind of VPN.
pub(crate) fn profile(
    connection: &HashMap<String, HashMap<String, OwnedValue>>,
    uuid: &str,
    name: &str,
) -> Option<Profile> {
    let vpn = connection.get("vpn")?;
    let service_type = string(vpn.get("service-type")?)?;
    if service_type != SERVICE_TYPE {
        return None;
    }

    let data = data_dict(vpn.get("data")?)?;
    Some(Profile {
        uuid: String::from(uuid),
        name: String::from(name),
        gateway: data.get("gateway").cloned().unwrap_or_default(),
        // NM omits the key entirely on a profile created before the protocol
        // picker existed, and openconnect's own default is AnyConnect.
        protocol: data
            .get("protocol")
            .cloned()
            .unwrap_or_else(|| String::from("anyconnect")),
        username: data.get(USERNAME_KEY).cloned().filter(|u| !u.is_empty()),
    })
}

fn string(value: &OwnedValue) -> Option<String> {
    String::try_from(value.clone()).ok()
}

fn data_dict(value: &OwnedValue) -> Option<HashMap<String, String>> {
    HashMap::<String, String>::try_from(value.clone()).ok()
}

/// Whether wayle can produce this profile's secrets natively.
///
/// Only GlobalProtect for now. Anything else falls through to the normal
/// no-secrets answer, which lets another agent — or the plugin's own auth
/// dialog, if it is installed — have a go rather than failing outright.
pub(crate) fn is_supported(profile: &Profile) -> bool {
    profile.protocol == "gp" && !profile.gateway.is_empty()
}

/// Authenticates and returns the secrets the openconnect plugin asked for.
///
/// `request_new` is NM telling us the secrets it had were rejected: the cached
/// cookie and the stored password are dropped and the user signs in afresh.
///
/// # Errors
///
/// Returns an error when the gateway refuses the credentials, when it demands
/// an authentication method wayle does not implement, or when the user
/// dismisses the prompt.
pub(crate) async fn authenticate(
    profile: &Profile,
    request_new: bool,
    state: &SecretAgentState,
) -> Result<HashMap<String, String>, Error> {
    if let Some(session) = reusable_session(profile, request_new) {
        return Ok(hand_out(&profile.uuid, &session));
    }

    let client = client()?;
    let (username, password, password_was_typed) = credentials(profile, state, &client).await?;
    // A failed sign-in drops the stored password: the likeliest thing that
    // went stale, and keeping it would make every later attempt fail the same
    // way with no way for the user to correct it.
    let session = sign_in(profile, &client, &username, &password, state)
        .await
        .inspect_err(|_| cache::forget_password(&profile.uuid))?;

    cache::store_session(&profile.uuid, &session);
    if password_was_typed {
        cache::store_password(&profile.uuid, &password);
    }
    info!(name = %profile.name, "VPN sign-in complete");
    Ok(hand_out(&profile.uuid, &session))
}

/// Records which cookie NM is being given, so the next request for the same
/// profile can tell a rejected cookie from a fresh reconnect.
fn hand_out(uuid: &str, session: &gp::Session) -> HashMap<String, String> {
    if let Ok(mut handed) = HANDED_OUT.lock() {
        handed.insert(String::from(uuid), (session.cookie.clone(), Instant::now()));
    }
    secrets(session)
}

/// Forgets everything cached for a profile: the session, the password, and
/// the record of what was last handed over.
///
/// Called when the profile is deleted. A session cookie and a password
/// outliving the profile they belong to is a leak, and a profile recreated
/// under the same name gets a new UUID, so nothing would ever collect them.
pub(crate) fn forget(uuid: &str) {
    cache::forget_session(uuid);
    cache::forget_password(uuid);
    if let Ok(mut handed) = HANDED_OUT.lock() {
        handed.remove(uuid);
    }
}

/// The cached session to reuse, or `None` when there is none to reuse — either
/// because nothing is cached or because NM has just told us what was cached
/// did not work.
fn reusable_session(profile: &Profile, request_new: bool) -> Option<gp::Session> {
    if request_new {
        debug!(name = %profile.name, "previous VPN credentials rejected, discarding them");
        cache::forget_session(&profile.uuid);
        cache::forget_password(&profile.uuid);
        return None;
    }

    let session = cache::session(&profile.uuid)?;
    let spent = HANDED_OUT
        .lock()
        .ok()
        .is_some_and(|handed| is_spent(handed.get(&profile.uuid), &session.cookie, Instant::now()));
    if spent {
        info!(name = %profile.name, "the cached VPN cookie was just refused; signing in again");
        cache::forget_session(&profile.uuid);
        return None;
    }

    info!(name = %profile.name, "reusing cached VPN session; no sign-in needed");
    Some(session)
}

/// Whether NM is asking again for a cookie it was given moments ago — which
/// only happens when whatever it was given did not work.
///
/// The window is what separates a retry from a legitimate reconnect: a tunnel
/// coming back after a suspend asks minutes or hours later, and must get the
/// cached cookie rather than a fresh second factor.
fn is_spent(handed: Option<&(String, Instant)>, cookie: &str, now: Instant) -> bool {
    handed
        .is_some_and(|(previous, at)| previous == cookie && now.duration_since(*at) < RETRY_WINDOW)
}

/// Posts the login and follows however many challenge rounds the gateway
/// asks for, up to [`MAX_CHALLENGES`].
async fn sign_in(
    profile: &Profile,
    client: &reqwest::Client,
    username: &str,
    password: &str,
    state: &SecretAgentState,
) -> Result<gp::Session, Error> {
    let computer = hostname();
    let mut input_str = String::new();
    let mut answer = String::from(password);

    for _ in 0..=MAX_CHALLENGES {
        match gp::login(
            client,
            &profile.gateway,
            username,
            &answer,
            &computer,
            &input_str,
        )
        .await?
        {
            gp::Step::Authenticated(session) => return Ok(session),
            gp::Step::Challenge {
                prompt,
                input_str: token,
            } => {
                answer = challenge(profile, &prompt, state).await?;
                input_str = token;
            }
        }
    }

    Err(Error::VpnAuthenticationFailed(String::from(
        "the gateway kept asking for more factors",
    )))
}

/// The secrets the openconnect plugin consumes — all three of them.
///
/// `gwcert` is not optional, whatever its being a pin of an already-trusted
/// certificate suggests: the plugin's own `need_secrets` counts the key as
/// missing until it is present, so a reply without it makes NM report "final
/// secrets request failed to provide sufficient secrets" and never launch
/// openconnect at all. Trust and the plugin's key set are separate questions.
fn secrets(session: &gp::Session) -> HashMap<String, String> {
    HashMap::from([
        (String::from("cookie"), session.cookie.clone()),
        (String::from("gateway"), session.host.clone()),
        (String::from("gwcert"), session.gwcert.clone()),
    ])
}

/// The username and password to sign in with, asking only for what is missing.
///
/// The gateway is asked what it wants before the user is: its prelogin
/// response carries the field labels and the instruction an administrator
/// wrote, and it is where a SAML gateway is caught — before any credentials
/// have been posted at it.
async fn credentials(
    profile: &Profile,
    state: &SecretAgentState,
    client: &reqwest::Client,
) -> Result<(String, String, bool), Error> {
    let stored_user = profile.username.clone();
    let stored_password = cache::password(&profile.uuid);

    // Nothing to ask, so nothing to ask the gateway about either.
    if let (Some(user), Some(password)) = (&stored_user, &stored_password) {
        return Ok((user.clone(), password.clone(), false));
    }

    let prelogin = gp::prelogin(client, &profile.gateway).await?;

    let mut fields = Vec::new();
    if stored_user.is_none() {
        fields.push(SecretField {
            key: String::from("user"),
            label: prelogin.username_label.clone(),
            secret: false,
        });
    }
    if stored_password.is_none() {
        fields.push(SecretField {
            key: String::from("passwd"),
            label: prelogin.password_label.clone(),
            secret: true,
        });
    }

    let values = state
        .prompt(SecretRequest {
            uuid: profile.uuid.clone(),
            name: profile.name.clone(),
            setting: String::from("vpn"),
            message: prelogin.message.clone(),
            fields,
        })
        .await
        .ok_or_else(cancelled)?;

    let username = stored_user
        .or_else(|| values.get("user").cloned())
        .filter(|user| !user.is_empty())
        .ok_or_else(|| Error::VpnAuthenticationFailed(String::from("no username given")))?;
    let typed = stored_password.is_none();
    let password = stored_password
        .or_else(|| values.get("passwd").cloned())
        .unwrap_or_default();

    Ok((username, password, typed))
}

/// Asks the user for a second factor, in the gateway's own words.
async fn challenge(
    profile: &Profile,
    prompt: &str,
    state: &SecretAgentState,
) -> Result<String, Error> {
    debug!(name = %profile.name, "gateway issued a challenge");
    let values = state
        .prompt(SecretRequest {
            uuid: profile.uuid.clone(),
            name: profile.name.clone(),
            setting: String::from("vpn"),
            message: Some(if prompt.is_empty() {
                String::from("Additional authentication required")
            } else {
                String::from(prompt)
            }),
            fields: vec![SecretField {
                key: String::from("passwd"),
                label: String::from("Code"),
                secret: true,
            }],
        })
        .await
        .ok_or_else(cancelled)?;

    Ok(values.get("passwd").cloned().unwrap_or_default())
}

fn cancelled() -> Error {
    Error::VpnAuthenticationFailed(String::from("sign-in dismissed"))
}

/// The HTTPS client the sign-in runs on.
///
/// No overall timeout: the per-request ones differ by an order of magnitude
/// (see [`LOGIN_TIMEOUT`]) and a client-wide one would cap them all at the
/// shortest. `tls_info` is what makes the gateway's certificate readable off
/// the response, which is where the `gwcert` secret comes from.
pub(super) fn client() -> Result<reqwest::Client, Error> {
    reqwest::Client::builder()
        .user_agent(USER_AGENT)
        .connect_timeout(CONNECT_TIMEOUT)
        .tls_info(true)
        .build()
        .map_err(|error| {
            Error::VpnAuthenticationFailed(format!("cannot build the HTTPS client: {error}"))
        })
}

/// The `computer` field a gateway records the session against.
fn hostname() -> String {
    std::fs::read_to_string("/proc/sys/kernel/hostname")
        .ok()
        .map(|raw| raw.trim().to_owned())
        .filter(|name| !name.is_empty())
        .unwrap_or_else(|| {
            warn!("cannot read the hostname; reporting a placeholder to the VPN gateway");
            String::from("localhost")
        })
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use zbus::zvariant::{OwnedValue, Value};

    use super::*;

    fn connection(
        service: &str,
        data: &[(&str, &str)],
    ) -> HashMap<String, HashMap<String, OwnedValue>> {
        let dict: HashMap<String, String> = data
            .iter()
            .map(|(key, value)| (String::from(*key), String::from(*value)))
            .collect();
        HashMap::from([(
            String::from("vpn"),
            HashMap::from([
                (
                    String::from("service-type"),
                    OwnedValue::try_from(Value::from(service)).expect("string value"),
                ),
                (
                    String::from("data"),
                    OwnedValue::try_from(Value::from(dict)).expect("dict value"),
                ),
            ]),
        )])
    }

    #[test]
    fn an_openconnect_profile_is_read_out_of_nms_dictionary() {
        let profile = profile(
            &connection(
                SERVICE_TYPE,
                &[
                    ("gateway", "vpn.example.com"),
                    ("protocol", "gp"),
                    (USERNAME_KEY, "alice"),
                ],
            ),
            "uuid-1",
            "Work",
        )
        .expect("an openconnect profile is recognised");

        assert_eq!(profile.gateway, "vpn.example.com");
        assert_eq!(profile.protocol, "gp");
        assert_eq!(profile.username.as_deref(), Some("alice"));
        assert!(is_supported(&profile));
    }

    #[test]
    fn another_plugins_profile_is_not_ours() {
        assert_eq!(
            profile(
                &connection(
                    "org.freedesktop.NetworkManager.openvpn",
                    &[("gateway", "x")]
                ),
                "uuid-1",
                "Work",
            ),
            None
        );
        assert_eq!(profile(&HashMap::new(), "uuid-1", "Work"), None);
    }

    #[test]
    fn a_non_globalprotect_openconnect_profile_is_left_to_someone_else() {
        // AnyConnect and friends are the same plugin but a different sign-in;
        // claiming them would break VPNs that work today via the auth dialog.
        let profile = profile(
            &connection(
                SERVICE_TYPE,
                &[("gateway", "vpn.example.com"), ("protocol", "anyconnect")],
            ),
            "uuid-1",
            "Work",
        )
        .expect("still an openconnect profile");
        assert!(!is_supported(&profile));
    }

    #[test]
    fn a_profile_with_no_protocol_defaults_the_way_openconnect_does() {
        let profile = profile(
            &connection(SERVICE_TYPE, &[("gateway", "vpn.example.com")]),
            "uuid-1",
            "Work",
        )
        .expect("profile");
        assert_eq!(profile.protocol, "anyconnect");
        assert!(!is_supported(&profile));
    }

    #[test]
    fn a_globalprotect_profile_with_no_gateway_cannot_be_signed_into() {
        let profile = profile(
            &connection(SERVICE_TYPE, &[("protocol", "gp")]),
            "uuid-1",
            "Work",
        )
        .expect("profile");
        assert!(!is_supported(&profile));
    }

    fn session() -> gp::Session {
        gp::Session {
            cookie: String::from("authcookie=abc"),
            host: String::from("vpn.example.com"),
            gwcert: String::from("pin-sha256:AAAA"),
        }
    }

    #[test]
    fn the_secrets_are_exactly_the_three_keys_the_plugin_asks_for() {
        // Pinned as a set, not key by key: one missing key makes NM retry
        // silently rather than error, which is a loop with no message in it.
        let secrets = secrets(&session());
        let mut keys: Vec<&str> = secrets.keys().map(String::as_str).collect();
        keys.sort_unstable();
        assert_eq!(keys, ["cookie", "gateway", "gwcert"]);

        assert_eq!(
            secrets.get("cookie").map(String::as_str),
            Some("authcookie=abc")
        );
        assert_eq!(
            secrets.get("gateway").map(String::as_str),
            Some("vpn.example.com")
        );
        assert_eq!(
            secrets.get("gwcert").map(String::as_str),
            Some("pin-sha256:AAAA")
        );
    }

    #[test]
    fn a_cookie_asked_for_again_moments_later_is_treated_as_refused() {
        // NM re-asking straight away means the plugin would not take what it
        // was given. Handing the same cookie back is the loop this prevents.
        let now = Instant::now();
        let handed = (String::from("authcookie=abc"), now);
        assert!(is_spent(Some(&handed), "authcookie=abc", now));
    }

    #[test]
    fn a_reconnect_much_later_still_reuses_its_cached_cookie() {
        // The whole point of the cache: a tunnel coming back after a suspend
        // must not cost the user another second factor.
        let handed = (
            String::from("authcookie=abc"),
            Instant::now() - RETRY_WINDOW - Duration::from_secs(1),
        );
        assert!(!is_spent(Some(&handed), "authcookie=abc", Instant::now()));
        // Nor does a profile nothing was ever handed out for.
        assert!(!is_spent(None, "authcookie=abc", Instant::now()));
        // Nor a cookie that is not the one that was handed out.
        let fresh = (String::from("authcookie=old"), Instant::now());
        assert!(!is_spent(Some(&fresh), "authcookie=new", Instant::now()));
    }
}
