//! Array Networks sign-in.
//!
//! The shortest of the family, and the shapes come from openconnect's
//! `array.c` rather than from guesswork:
//!
//! 1. `POST prx/000/http/localhost/login`, form-urlencoded, with `method`
//!    (the auth group), `uname` and `pwd` — the field names are Array's own,
//!    not the `username`/`password` the other protocols use;
//! 2. success is a cookie whose name *starts with* `ANsession`, and
//!    openconnect's `--cookie` is that cookie's own name and value. The
//!    prefix match is deliberate: the name carries a suffix that varies.
//!
//! There is no challenge round. A gateway that wants a second factor is not
//! something this dialect can describe, so it reads as a refusal — which is
//! the honest answer, since nothing here could answer one.
//!
//! Not verified against a real Array gateway. `MODE=array` in
//! `tests/mock-gateway/gateway.py` encodes these assumptions and cannot
//! falsify them; the likeliest thing to be wrong is `method`, which
//! openconnect prompts for as "authgroup" and which wayle sends empty when
//! the profile says nothing.

use super::{LOGIN_TIMEOUT, Session, SignIn, form, peer_pin};
use crate::{
    Error,
    agent::SecretAgentState,
    types::agent::{SecretField, SecretRequest},
    vpn::openconnect::Profile,
};

/// The prefix of the cookie a successful sign-in sets.
const COOKIE_PREFIX: &str = "ANsession";

/// Signs in and returns the session for the plugin.
///
/// # Errors
///
/// Returns an error when the gateway refuses the credentials or the user
/// dismisses the prompt.
pub(super) async fn sign_in(
    profile: &Profile,
    client: &reqwest::Client,
    state: &SecretAgentState,
) -> Result<SignIn, Error> {
    let username = profile.username.clone().unwrap_or_default();
    let stored = super::cache::password(&profile.uuid);

    let (username, password, remember) = match (username.is_empty(), stored) {
        (false, Some(password)) => (username, password, None),
        _ => {
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
            let password = values.get("password").cloned().unwrap_or_default();
            (
                values.get("username").cloned().unwrap_or(username),
                password.clone(),
                Some(password).filter(|value| !value.is_empty()),
            )
        }
    };

    let body = request_body(&username, &password);
    let response = client
        .post(format!(
            "https://{}/prx/000/http/localhost/login",
            profile.gateway
        ))
        .header(
            reqwest::header::CONTENT_TYPE,
            "application/x-www-form-urlencoded",
        )
        .timeout(LOGIN_TIMEOUT)
        .body(body)
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

    if !status.is_success() && !status.is_redirection() {
        return Err(unsupported(&format!(
            "the gateway answered {status} to an Array authentication"
        )));
    }
    let cookie = cookie.ok_or_else(|| auth_error("the gateway refused the credentials"))?;

    Ok(SignIn {
        session: Session {
            cookie,
            host: profile.gateway.clone(),
            gwcert,
        },
        remember_password: remember,
    })
}

/// The login body. Array's own field names, and an empty `method` when the
/// profile names no auth group — openconnect prompts for it, but there is
/// nothing to show a user who has never heard of it.
fn request_body(username: &str, password: &str) -> String {
    form::encode(&[("method", ""), ("uname", username), ("pwd", password)])
}

/// The `ANsession…=…` cookie the gateway set, as openconnect's `--cookie`
/// takes it: the cookie's own name and value.
///
/// Matched by prefix because the name carries a varying suffix, and an empty
/// value is a deletion rather than a session.
fn session_cookie(headers: &reqwest::header::HeaderMap) -> Option<String> {
    headers
        .get_all(reqwest::header::SET_COOKIE)
        .iter()
        .filter_map(|value| value.to_str().ok())
        .find_map(|cookie| {
            let pair = cookie.split(';').next()?.trim();
            let (name, value) = pair.split_once('=')?;
            let name = name.trim();
            (name.starts_with(COOKIE_PREFIX) && !value.trim().is_empty())
                .then(|| format!("{name}={}", value.trim()))
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
    fn the_session_cookie_is_matched_by_prefix_and_keeps_its_own_name() {
        // The suffix varies, so an exact match would find nothing — and the
        // name openconnect sends back is the full one, suffix included.
        let found = session_cookie(&headers(&["ANsession1234=abc; path=/; secure"]));
        assert_eq!(found.as_deref(), Some("ANsession1234=abc"));
        assert_eq!(
            session_cookie(&headers(&["ANsession=xyz"])).as_deref(),
            Some("ANsession=xyz")
        );
    }

    #[test]
    fn a_cookie_that_is_not_a_session_is_not_taken() {
        assert!(session_cookie(&headers(&["ANsession1234=; Max-Age=0"])).is_none());
        assert!(session_cookie(&headers(&["JSESSIONID=abc"])).is_none());
        // A name that merely *contains* the prefix is not one that starts
        // with it.
        assert!(session_cookie(&headers(&["notANsession=abc"])).is_none());
        assert!(session_cookie(&headers(&[])).is_none());
    }

    #[test]
    fn the_login_body_uses_arrays_own_field_names() {
        // `uname`/`pwd`, not `username`/`password`: the other protocols'
        // names would simply be ignored by an Array gateway.
        let body = request_body("alice", "p@ss&word");
        assert!(body.contains("uname=alice"), "{body}");
        assert!(body.contains("pwd=p%40ss%26word"), "{body}");
        assert!(body.contains("method="), "{body}");
        assert!(
            !body.contains("username=") && !body.contains("password="),
            "{body}"
        );
    }
}

/// End-to-end against the mock Array gateway in `tests/mock-gateway`.
///
/// `#[ignore]`d because it needs the containers running: `just test-gateway`.
#[cfg(test)]
#[allow(unsafe_code)]
mod mock {
    use std::{collections::HashMap, sync::Arc};

    use futures::StreamExt;

    use super::*;

    const GATEWAY: &str = "127.0.0.1:8447";
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
            uuid: String::from("mock-array"),
            name: String::from("Mock"),
            gateway: String::from(GATEWAY),
            protocol: String::from("array"),
            username: None,
            sso: false,
            plugin_signin: false,
        }
    }

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
    async fn a_sign_in_comes_back_with_the_session_cookie_and_the_pin() {
        let state = Arc::new(SecretAgentState::new());
        answer_prompts(
            &state,
            HashMap::from([("username", "alice"), ("password", "hunter2")]),
        );

        let signed_in = sign_in(&profile(), &client(), &state)
            .await
            .expect("the mock gateway signs us in");

        assert_eq!(signed_in.session.cookie, "ANsession1234=ARRAYSESSION");
        assert_eq!(signed_in.session.host, GATEWAY);
        assert_eq!(signed_in.session.gwcert, PIN);
        assert_eq!(signed_in.remember_password.as_deref(), Some("hunter2"));
    }

    #[tokio::test]
    #[ignore = "needs the mock gateway: just test-gateway"]
    async fn a_wrong_password_is_a_refusal_and_not_an_unsupported_protocol() {
        let state = Arc::new(SecretAgentState::new());
        answer_prompts(
            &state,
            HashMap::from([("username", "alice"), ("password", "wrong")]),
        );

        let error = sign_in(&profile(), &client(), &state)
            .await
            .expect_err("a bad password does not sign anyone in");
        assert!(
            matches!(error, Error::VpnAuthenticationFailed(_)),
            "got: {error}"
        );
    }

    #[tokio::test]
    #[ignore = "needs the mock gateway: just test-gateway"]
    async fn a_globalprotect_gateway_is_not_mistaken_for_an_array_one() {
        let state = Arc::new(SecretAgentState::new());
        let mut profile = profile();
        profile.gateway = String::from("127.0.0.1:8443");
        answer_prompts(
            &state,
            HashMap::from([("username", "alice"), ("password", "hunter2")]),
        );

        let error = sign_in(&profile, &client(), &state)
            .await
            .expect_err("that gateway does not speak Array");
        assert!(
            matches!(error, Error::VpnProtocolUnsupported(_)),
            "got: {error}"
        );
    }
}
