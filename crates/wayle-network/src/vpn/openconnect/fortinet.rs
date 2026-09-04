//! Fortinet SSL VPN sign-in.
//!
//! The wire shapes come from openconnect's `fortinet.c`, which is the only
//! written-down description of them there is.
//!
//! Unlike GlobalProtect and AnyConnect this is not XML at all — it is an
//! ordinary HTML login form:
//!
//! 1. `POST /remote/logincheck` with `username`, `credential`, `realm` and
//!    `ajax=1&just_logged_in=1`, form-urlencoded;
//! 2. success is signalled by a `Set-Cookie: SVPNCOOKIE=…`, and that cookie
//!    *is* openconnect's `--cookie`;
//! 3. a 200 with a body of `ret=…,tokeninfo=…` is a second factor. The reply
//!    sends `code` instead of `credential` and parrots back
//!    `reqid,polid,grp,portal,peer,magic` from that body untouched — the
//!    gateway uses them to recognise the conversation. `chal_msg=` is the
//!    prompt to show.
//!
//! One deliberate quirk carried over: for `tokeninfo=ftm_push` with an empty
//! code, `magic` is dropped and `ftmpush=1` added, which is what asks the
//! gateway to send a mobile push instead of expecting a typed code.
//!
//! Not verified against a real FortiGate. The mock in
//! `tests/mock-gateway/gateway.py` (`MODE=fortinet`) encodes these
//! assumptions and cannot falsify them; the first things to check against a
//! real one are, in order of how likely they are to be wrong:
//!
//! 1. whether `realm` must be absent rather than empty when there is none;
//! 2. whether the parroted field set is complete for that firmware — a
//!    missing one shows up as the challenge simply being asked again;
//! 3. whether the cookie is always `SVPNCOOKIE` (the name is matched exactly).

use tracing::debug;

use super::{LOGIN_TIMEOUT, MAX_CHALLENGES, Session, SignIn, form, peer_pin};
use crate::{
    Error,
    agent::SecretAgentState,
    types::agent::{SecretField, SecretRequest},
    vpn::openconnect::Profile,
};

/// The cookie a successful sign-in sets, and openconnect's `--cookie` for
/// this protocol.
const COOKIE_NAME: &str = "SVPNCOOKIE";

/// The values a challenge response carries that the next request has to send
/// back. `magic` is last on purpose: the `ftmpush` case truncates the body at
/// it.
const PARROTED: &[&str] = &["reqid", "polid", "grp", "portal", "peer", "magic"];

/// What the gateway wants next.
#[derive(Debug, PartialEq, Eq)]
enum Step {
    /// It set the session cookie; the sign-in is done.
    Authenticated(String),
    /// It wants a second factor.
    Challenge(Challenge),
}

/// A second-factor round.
#[derive(Debug, Default, PartialEq, Eq)]
struct Challenge {
    /// What to show the user, when the gateway said anything.
    message: Option<String>,
    /// The `key=value` pairs to send back untouched.
    parroted: Vec<(String, String)>,
    /// Whether this is the mobile-push flow rather than a typed code.
    ftm_push: bool,
}

/// Signs in and returns the session for the plugin.
///
/// # Errors
///
/// Returns an error when the gateway refuses the credentials, when it asks
/// for more factors than [`MAX_CHALLENGES`], or when the user dismisses the
/// prompt.
pub(super) async fn sign_in(
    profile: &Profile,
    client: &reqwest::Client,
    state: &SecretAgentState,
) -> Result<SignIn, Error> {
    let mut remember_password = None;
    let mut challenge: Option<Challenge> = None;
    // Carried across rounds: the challenge posts `username` too, and the
    // profile may not hold one — it is whatever was typed on the first round.
    // Without this the second post arrives with an empty username and the
    // gateway refuses it as a bad credential.
    let mut username = profile.username.clone().unwrap_or_default();

    for _ in 0..=MAX_CHALLENGES {
        let answers = ask(profile, &username, challenge.as_ref(), state).await?;
        username.clone_from(&answers.username);
        if challenge.is_none() && remember_password.is_none() {
            remember_password = answers.password.clone();
        }

        let body = request_body(&answers, challenge.as_ref());
        let exchange = post(client, &profile.gateway, &body).await?;

        match exchange.step {
            Step::Authenticated(cookie) => {
                return Ok(SignIn {
                    session: Session {
                        cookie,
                        host: profile.gateway.clone(),
                        gwcert: exchange.gwcert,
                    },
                    remember_password,
                });
            }
            Step::Challenge(next) => {
                debug!(
                    ftm_push = next.ftm_push,
                    "the FortiGate asked for a second factor"
                );
                challenge = Some(next);
            }
        }
    }

    Err(auth_error("the gateway kept asking for more factors"))
}

/// What the user (or the cache, or the profile) supplied.
#[derive(Debug, Default)]
struct Answers {
    username: String,
    /// The password on the first round, the second-factor code after that.
    credential: String,
    /// The password, when this round asked for one and got a non-empty
    /// answer — a second factor is never worth remembering.
    password: Option<String>,
}

/// Collects the username and whatever secret this round needs.
async fn ask(
    profile: &Profile,
    known_username: &str,
    challenge: Option<&Challenge>,
    state: &SecretAgentState,
) -> Result<Answers, Error> {
    let username = String::from(known_username);
    let stored = super::cache::password(&profile.uuid);

    // A challenge round always asks: the code is different every time, and a
    // remembered one is guaranteed stale.
    if let Some(challenge) = challenge {
        // The push flow expects an empty code, so the prompt is allowed to
        // come back blank rather than being treated as a dismissal.
        let values = state
            .prompt(SecretRequest {
                uuid: profile.uuid.clone(),
                name: profile.name.clone(),
                setting: String::from("vpn"),
                message: challenge.message.clone(),
                fields: vec![SecretField {
                    key: String::from("code"),
                    label: String::from("Code"),
                    secret: true,
                }],
            })
            .await
            .ok_or_else(|| auth_error("sign-in dismissed"))?;
        return Ok(Answers {
            username,
            credential: values.get("code").cloned().unwrap_or_default(),
            password: None,
        });
    }

    // First round: the username can come from the profile and the password
    // from the cache, in which case nothing is asked at all.
    if !username.is_empty()
        && let Some(password) = stored
    {
        return Ok(Answers {
            username,
            credential: password,
            password: None,
        });
    }

    let mut fields = Vec::new();
    if username.is_empty() {
        fields.push(SecretField {
            key: String::from("username"),
            label: String::from("Username"),
            secret: false,
        });
    }
    fields.push(SecretField {
        key: String::from("password"),
        label: String::from("Password"),
        secret: true,
    });

    let values = state
        .prompt(SecretRequest {
            uuid: profile.uuid.clone(),
            name: profile.name.clone(),
            setting: String::from("vpn"),
            message: None,
            fields,
        })
        .await
        .ok_or_else(|| auth_error("sign-in dismissed"))?;

    let credential = values.get("password").cloned().unwrap_or_default();
    Ok(Answers {
        username: values.get("username").cloned().unwrap_or(username),
        password: Some(credential.clone()).filter(|value| !value.is_empty()),
        credential,
    })
}

/// The `POST /remote/logincheck` body for this round.
fn request_body(answers: &Answers, challenge: Option<&Challenge>) -> String {
    let Some(challenge) = challenge else {
        // The plain login form.
        let mut body = form::encode(&[
            ("username", answers.username.as_str()),
            ("credential", answers.credential.as_str()),
            ("realm", ""),
        ]);
        body.push_str("&ajax=1&just_logged_in=1");
        return body;
    };

    // A challenge: the code replaces the credential, and the gateway's own
    // values go back with it.
    let mut pairs: Vec<(&str, &str)> = vec![
        ("username", answers.username.as_str()),
        ("code", answers.credential.as_str()),
        ("realm", ""),
    ];
    // `magic` is dropped for a push with no code, which is the signal that
    // asks the gateway to push rather than to verify.
    let push = challenge.ftm_push && answers.credential.is_empty();
    for (key, value) in &challenge.parroted {
        if push && key == "magic" {
            continue;
        }
        pairs.push((key.as_str(), value.as_str()));
    }
    let mut body = form::encode(&pairs);
    if push {
        body.push_str("&ftmpush=1");
    }
    body
}

/// One round trip with the gateway.
struct Exchange {
    step: Step,
    gwcert: String,
}

async fn post(client: &reqwest::Client, gateway: &str, body: &str) -> Result<Exchange, Error> {
    let response = client
        .post(format!("https://{gateway}/remote/logincheck"))
        .header(
            reqwest::header::CONTENT_TYPE,
            "application/x-www-form-urlencoded",
        )
        .timeout(LOGIN_TIMEOUT)
        .body(String::from(body))
        .send()
        .await
        .map_err(|error| {
            Error::VpnAuthenticationFailed(format!("cannot reach the gateway: {error}"))
        })?;

    let gwcert = peer_pin(&response).ok_or_else(|| {
        auth_error("cannot read the gateway's certificate, which the VPN plugin requires")
    })?;
    let cookie = session_cookie(response.headers());
    let status = response.status();
    let body = response.text().await.map_err(|error| {
        Error::VpnAuthenticationFailed(format!("cannot read the gateway's reply: {error}"))
    })?;

    Ok(Exchange {
        step: parse(&body, cookie, status)?,
        gwcert,
    })
}

/// Reads the gateway's answer.
///
/// # Errors
///
/// Returns an error when the gateway rejected the credentials, and an
/// *unsupported* error for an answer this dialect does not know — a 401 with
/// an HTML body is openconnect's HTML-form 2FA, which wayle does not do, and
/// handing that back as a refusal would blame the user's password for it.
fn parse(body: &str, cookie: Option<String>, status: reqwest::StatusCode) -> Result<Step, Error> {
    // The cookie settles it whatever else the body says.
    if let Some(cookie) = cookie {
        return Ok(Step::Authenticated(cookie));
    }

    if status == reqwest::StatusCode::UNAUTHORIZED {
        return Err(unsupported(
            "this FortiGate wants an HTML sign-in form, which wayle cannot do",
        ));
    }
    if !status.is_success() {
        return Err(unsupported(&format!(
            "the gateway answered {status} to a Fortinet authentication"
        )));
    }

    let fields = parse_fields(body);
    if fields.iter().any(|(key, _)| key == "tokeninfo") {
        return Ok(Step::Challenge(Challenge {
            message: fields
                .iter()
                .find(|(key, _)| key == "chal_msg")
                .map(|(_, value)| value.clone()),
            // In `PARROTED` order rather than the order the gateway happened
            // to write them, so `magic` is last on the wire the way
            // openconnect sends it.
            parroted: PARROTED
                .iter()
                .filter_map(|wanted| {
                    fields
                        .iter()
                        .find(|(key, _)| key == wanted)
                        .map(|(key, value)| (key.clone(), value.clone()))
                })
                .collect(),
            ftm_push: fields
                .iter()
                .any(|(key, value)| key == "tokeninfo" && value == "ftm_push"),
        }));
    }

    // `ret=0` (or any non-1 ret) with no cookie is a refusal. Surface the
    // gateway's own wording where it gave any.
    Err(auth_error(
        &fields
            .iter()
            .find(|(key, _)| key == "err" || key == "chal_msg")
            .map(|(_, value)| value.clone())
            .unwrap_or_else(|| String::from("the gateway refused the credentials")),
    ))
}

/// Splits a Fortinet response body into its `key=value` pairs.
///
/// The body is one comma-separated line — `ret=1,redir=…,tokeninfo=…` — so
/// the split is on commas, not the `&` of a form.
fn parse_fields(body: &str) -> Vec<(String, String)> {
    let mut fields = Vec::new();
    // Ordered by `PARROTED` where it matters; collected in wire order here.
    for part in body.trim().split(',') {
        if let Some((key, value)) = part.split_once('=') {
            fields.push((key.trim().to_owned(), value.trim().to_owned()));
        }
    }
    fields
}

/// The `SVPNCOOKIE=…` a successful sign-in set, as openconnect's `--cookie`
/// wants it.
///
/// An empty value is a deletion, which is what the gateway sends before there
/// is a session — taking it would hand the plugin an empty cookie.
fn session_cookie(headers: &reqwest::header::HeaderMap) -> Option<String> {
    headers
        .get_all(reqwest::header::SET_COOKIE)
        .iter()
        .filter_map(|value| value.to_str().ok())
        .find_map(|cookie| {
            let pair = cookie.split(';').next()?.trim();
            let (name, value) = pair.split_once('=')?;
            (name.trim() == COOKIE_NAME && !value.trim().is_empty())
                .then(|| format!("{COOKIE_NAME}={}", value.trim()))
        })
}

fn auth_error(message: &str) -> Error {
    Error::VpnAuthenticationFailed(String::from(message))
}

fn unsupported(message: &str) -> Error {
    Error::VpnProtocolUnsupported(String::from(message))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn headers(cookies: &[&str]) -> reqwest::header::HeaderMap {
        let mut map = reqwest::header::HeaderMap::new();
        for cookie in cookies {
            map.append(
                reqwest::header::SET_COOKIE,
                reqwest::header::HeaderValue::from_str(cookie).expect("a valid header"),
            );
        }
        map
    }

    #[test]
    fn the_session_cookie_is_the_one_openconnect_asks_for() {
        let found = session_cookie(&headers(&[
            "SVPNCOOKIE=abc123; path=/; secure; HttpOnly",
            "other=x",
        ]));
        assert_eq!(found.as_deref(), Some("SVPNCOOKIE=abc123"));
    }

    #[test]
    fn an_emptied_cookie_is_a_deletion_and_not_a_session() {
        // The gateway clears it before there is a session; taking it would
        // hand the plugin an empty cookie and a tunnel that never comes up.
        assert!(session_cookie(&headers(&["SVPNCOOKIE=; path=/"])).is_none());
        assert!(session_cookie(&headers(&["SVPNCOOKIE=  ; Max-Age=0"])).is_none());
        // A different cookie is not this one.
        assert!(session_cookie(&headers(&["JSESSIONID=abc"])).is_none());
        assert!(session_cookie(&headers(&[])).is_none());
    }

    #[test]
    fn a_cookie_means_authenticated_whatever_the_body_says() {
        let step = parse(
            "ret=1,redir=/remote/fortisslvpn",
            Some(String::from("SVPNCOOKIE=abc")),
            reqwest::StatusCode::OK,
        )
        .expect("a cookie is a success");
        assert_eq!(step, Step::Authenticated(String::from("SVPNCOOKIE=abc")));
    }

    #[test]
    fn a_tokeninfo_body_is_a_challenge_and_carries_what_must_go_back() {
        let body = "ret=2,tokeninfo=,grp=Employees,reqid=17,polid=3,portal=web,\
                    peer=1,magic=deadbeef,chal_msg=Enter your token code";
        let step = parse(body, None, reqwest::StatusCode::OK).expect("a challenge");
        let Step::Challenge(challenge) = step else {
            panic!("expected a challenge");
        };

        assert_eq!(
            challenge.message.as_deref(),
            Some("Enter your token code"),
            "the gateway's own prompt is what the user should read"
        );
        assert!(!challenge.ftm_push);
        let keys: Vec<&str> = challenge
            .parroted
            .iter()
            .map(|(key, _)| key.as_str())
            .collect();
        // Every value the next request has to echo, and `magic` last so the
        // push case can truncate at it.
        assert_eq!(keys, ["reqid", "polid", "grp", "portal", "peer", "magic"]);
        assert!(
            !keys.contains(&"chal_msg") && !keys.contains(&"ret"),
            "only the gateway's own bookkeeping goes back: {keys:?}"
        );
    }

    #[test]
    fn a_refusal_surfaces_the_gateways_own_wording() {
        let error = parse("ret=0,err=Permission denied", None, reqwest::StatusCode::OK)
            .expect_err("no cookie and no challenge is a refusal");
        assert!(
            matches!(error, Error::VpnAuthenticationFailed(ref message) if message.contains("Permission denied")),
            "got {error:?}"
        );
    }

    #[test]
    fn an_html_form_gateway_is_reported_as_unsupported_not_as_a_bad_password() {
        // A refusal would blame the user's password and drop the cached one;
        // `VpnProtocolUnsupported` hands the profile back to the plugin's own
        // auth dialog instead.
        let error = parse("<html>…</html>", None, reqwest::StatusCode::UNAUTHORIZED)
            .expect_err("a 401 is not a success");
        assert!(
            matches!(error, Error::VpnProtocolUnsupported(_)),
            "got {error:?}"
        );
    }

    #[test]
    fn the_first_round_posts_the_plain_login_form() {
        let answers = Answers {
            username: String::from("alice"),
            credential: String::from("p@ss&word"),
            password: None,
        };
        let body = request_body(&answers, None);

        assert!(body.contains("username=alice"), "{body}");
        assert!(
            body.contains("credential=p%40ss%26word"),
            "a password with separators in it must arrive escaped: {body}"
        );
        assert!(body.contains("&ajax=1&just_logged_in=1"), "{body}");
    }

    #[test]
    fn a_challenge_round_sends_a_code_and_echoes_the_gateways_values() {
        let challenge = Challenge {
            message: None,
            parroted: vec![
                (String::from("reqid"), String::from("17")),
                (String::from("magic"), String::from("deadbeef")),
            ],
            ftm_push: false,
        };
        let answers = Answers {
            username: String::from("alice"),
            credential: String::from("123456"),
            password: None,
        };
        let body = request_body(&answers, Some(&challenge));

        assert!(body.contains("code=123456"), "{body}");
        assert!(
            !body.contains("credential="),
            "the code replaces the credential: {body}"
        );
        assert!(body.contains("reqid=17"), "{body}");
        assert!(body.contains("magic=deadbeef"), "{body}");
        assert!(!body.contains("ftmpush"), "{body}");
    }

    #[test]
    fn a_push_with_no_code_drops_magic_and_asks_for_the_push() {
        // openconnect's own quirk: this exact combination is what tells the
        // gateway to send a notification rather than verify a typed code.
        let challenge = Challenge {
            message: None,
            parroted: vec![
                (String::from("reqid"), String::from("17")),
                (String::from("magic"), String::from("deadbeef")),
            ],
            ftm_push: true,
        };
        let body = request_body(
            &Answers {
                username: String::from("alice"),
                credential: String::new(),
                password: None,
            },
            Some(&challenge),
        );

        assert!(body.ends_with("&ftmpush=1"), "{body}");
        assert!(!body.contains("magic="), "magic must be dropped: {body}");
        assert!(body.contains("reqid=17"), "{body}");

        // With a code typed, it is an ordinary challenge again.
        let typed = request_body(
            &Answers {
                username: String::from("alice"),
                credential: String::from("123456"),
                password: None,
            },
            Some(&challenge),
        );
        assert!(typed.contains("magic=deadbeef"), "{typed}");
        assert!(!typed.contains("ftmpush"), "{typed}");
    }

    #[test]
    fn a_response_body_splits_on_commas_not_ampersands() {
        // It is not a form body, despite the request being one.
        let fields = parse_fields("ret=1,redir=/remote/x,tokeninfo=ftm_push");
        assert_eq!(fields.len(), 3);
        assert_eq!(fields[0], (String::from("ret"), String::from("1")));
        assert_eq!(
            fields[2],
            (String::from("tokeninfo"), String::from("ftm_push"))
        );
        // Junk without an `=` is skipped rather than producing empty keys.
        assert!(parse_fields("garbage").is_empty());
        assert!(parse_fields("").is_empty());
    }
}

/// End-to-end against the mock FortiGate in `tests/mock-gateway`.
///
/// `#[ignore]`d because they need the containers running: `just test-gateway`.
/// They speak the real protocol over a real TLS connection, which is the only
/// way to cover the `gwcert` pin.
#[cfg(test)]
#[allow(unsafe_code)]
mod mock {
    use std::{collections::HashMap, sync::Arc};

    use futures::StreamExt;

    use super::*;

    const GATEWAY: &str = "127.0.0.1:8446";
    const PIN: &str = "pin-sha256:eQO9gC6TVZtfFqt1YHSe7HUSxgHyRmhNo3UXeSAxvZI=";

    fn client() -> reqwest::Client {
        // SAFETY: nextest runs every test in its own process.
        unsafe {
            std::env::set_var(
                "SSL_CERT_FILE",
                concat!(env!("CARGO_MANIFEST_DIR"), "/tests/mock-gateway/ca.crt"),
            );
        }
        super::super::client().expect("the client builds")
    }

    fn profile() -> Profile {
        Profile {
            uuid: String::from("mock-fortinet"),
            name: String::from("Mock"),
            gateway: String::from(GATEWAY),
            protocol: String::from("fortinet"),
            username: None,
            sso: false,
        }
    }

    /// Answers whatever the gateway asks for, the way the user would.
    fn answer_prompts(state: &Arc<SecretAgentState>, answers: HashMap<&'static str, &'static str>) {
        let state = Arc::clone(state);
        tokio::spawn(async move {
            let mut changes = state.request.watch();
            while let Some(change) = changes.next().await {
                let Some(request) = change else { continue };
                let values = request
                    .fields
                    .iter()
                    .map(|field| {
                        (
                            field.key.clone(),
                            String::from(answers.get(field.key.as_str()).copied().unwrap_or("")),
                        )
                    })
                    .collect();
                state.submit(values).await;
            }
        });
    }

    #[tokio::test]
    #[ignore = "needs the mock gateway: just test-gateway"]
    async fn a_full_sign_in_answers_the_form_the_challenge_and_comes_back_with_a_cookie() {
        let state = Arc::new(SecretAgentState::new());
        answer_prompts(
            &state,
            HashMap::from([
                ("username", "alice"),
                ("password", "hunter2"),
                ("code", "123456"),
            ]),
        );

        let signed_in = sign_in(&profile(), &client(), &state)
            .await
            .expect("the mock gateway signs us in");

        assert_eq!(signed_in.session.cookie, "SVPNCOOKIE=SVPNSESSIONVALUE");
        assert_eq!(signed_in.session.host, GATEWAY);
        assert_eq!(signed_in.session.gwcert, PIN);
        // The password is worth caching; the token code never is.
        assert_eq!(signed_in.remember_password.as_deref(), Some("hunter2"));
    }

    #[tokio::test]
    #[ignore = "needs the mock gateway: just test-gateway"]
    async fn the_challenge_round_has_to_echo_the_gateways_own_values() {
        // The mock refuses a challenge reply that does not carry `reqid` and
        // `magic` back, which is what proves the echo actually happens rather
        // than the mock being lenient.
        let state = Arc::new(SecretAgentState::new());
        answer_prompts(
            &state,
            HashMap::from([
                ("username", "alice"),
                ("password", "hunter2"),
                ("code", "123456"),
            ]),
        );
        let signed_in = sign_in(&profile(), &client(), &state)
            .await
            .expect("the echo is what the gateway recognises us by");
        assert!(signed_in.session.cookie.starts_with("SVPNCOOKIE="));
    }

    #[tokio::test]
    #[ignore = "needs the mock gateway: just test-gateway"]
    async fn a_wrong_password_is_the_gateways_own_refusal() {
        let state = Arc::new(SecretAgentState::new());
        answer_prompts(
            &state,
            HashMap::from([("username", "alice"), ("password", "wrong")]),
        );

        let error = sign_in(&profile(), &client(), &state)
            .await
            .expect_err("a bad password does not sign anyone in");
        assert!(
            error.to_string().contains("Permission denied"),
            "got: {error}"
        );
        // A refusal must reach the user rather than being handed to the next
        // agent as "wayle does not speak this".
        assert!(matches!(error, Error::VpnAuthenticationFailed(_)));
    }

    #[tokio::test]
    #[ignore = "needs the mock gateway: just test-gateway"]
    async fn a_globalprotect_gateway_is_not_mistaken_for_a_fortigate() {
        // 8443 is the GlobalProtect mock: it has no /remote/logincheck, so
        // this has to reach NM as "someone else should try".
        let state = Arc::new(SecretAgentState::new());
        let mut profile = profile();
        profile.gateway = String::from("127.0.0.1:8443");
        answer_prompts(
            &state,
            HashMap::from([("username", "alice"), ("password", "hunter2")]),
        );

        let error = sign_in(&profile, &client(), &state)
            .await
            .expect_err("that gateway does not speak Fortinet");
        assert!(
            matches!(error, Error::VpnProtocolUnsupported(_)),
            "got: {error}"
        );
    }
}
