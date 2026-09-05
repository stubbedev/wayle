//! Sign-in for the protocols that authenticate through an HTML login page:
//! Juniper (`nc`), Pulse Connect Secure (`pulse`) and F5 BIG-IP (`f5`).
//!
//! The page handling — reading a form, filling it, following whatever the
//! gateway asks next — is [`web_login`](super::web_login). This is the part
//! that knows where each protocol's login page lives and what its session
//! cookie is called, and that drives the exchange.
//!
//! # What is and is not verified
//!
//! Pinned by the tests: the endpoints and cookie names per protocol, that a
//! gateway which stops asking without setting a cookie is a failure rather
//! than a success, and the loop bound. The page mechanics have their own tests
//! next door.
//!
//! **Not** verified: any real gateway, because none is available to point this
//! at. See [`web_login`](super::web_login) for why the markup is read rather
//! than known.

use reqwest::header::SET_COOKIE;

use super::{
    LOGIN_TIMEOUT, Profile, Session, SignIn, peer_pin, web_login,
    web_login::{Page, auth_error},
};
use crate::{
    Error,
    agent::SecretAgentState,
    types::agent::{SecretField, SecretRequest},
};

/// What one of these protocols needs to know about itself.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) struct Dialect {
    /// Path of the login page, relative to the gateway.
    pub login_path: &'static str,
    /// The cookie whose value is the session.
    pub cookie: &'static str,
}

/// Juniper and Pulse share a login: both are the `dana-na` web front end, and
/// openconnect's Pulse support authenticates through the same pages before it
/// switches to its own transport.
const JUNIPER: Dialect = Dialect {
    login_path: "/dana-na/auth/url_default/welcome.cgi",
    cookie: "DSID",
};

/// F5 BIG-IP APM: one policy endpoint, and `MRHSession` once it is satisfied.
const F5: Dialect = Dialect {
    login_path: "/my.policy",
    cookie: "MRHSession",
};

/// The dialect for a protocol, or `None` when this module does not speak it.
pub(super) fn dialect(protocol: &str) -> Option<Dialect> {
    match protocol {
        "nc" | "pulse" => Some(JUNIPER),
        "f5" => Some(F5),
        _ => None,
    }
}

/// Signs in through the gateway's web login and returns the session.
///
/// # Errors
///
/// Returns an error when the gateway is unreachable, refuses the credentials,
/// keeps asking past the page limit, or finishes without a session cookie.
pub(super) async fn sign_in(
    profile: &Profile,
    client: &reqwest::Client,
    state: &SecretAgentState,
) -> Result<SignIn, Error> {
    let dialect = dialect(&profile.protocol)
        .ok_or_else(|| auth_error("no web sign-in for this openconnect protocol"))?;

    let mut url = format!("https://{}{}", profile.gateway, dialect.login_path);

    // Pinned from whichever response is in hand, so it ends up being the
    // certificate the host presented while it was issuing the cookie.
    let mut gwcert: Option<String> = None;
    let mut credentials: Option<(String, String, Option<String>)> = None;
    let mut cookie: Option<String> = None;
    let mut body: Option<String> = None;

    for _ in 0..web_login::max_pages() {
        let response = match &body {
            Some(body) => {
                client
                    .post(&url)
                    .header(
                        reqwest::header::CONTENT_TYPE,
                        "application/x-www-form-urlencoded",
                    )
                    .timeout(LOGIN_TIMEOUT)
                    .body(body.clone())
                    .send()
                    .await
            }
            None => client.get(&url).timeout(LOGIN_TIMEOUT).send().await,
        }
        .map_err(|error| auth_error(&format!("cannot reach the gateway: {error}")))?;

        // Read before the body: the session cookie is set on whichever
        // response finally accepts the sign-in, which is usually the one that
        // redirects away from the login pages.
        if let Some(pin) = peer_pin(&response) {
            gwcert = Some(pin);
        }
        let headers = set_cookies(&response);
        if let Some(found) = web_login::session_cookie(&headers, dialect.cookie) {
            cookie = Some(found);
        }
        let page_url = response.url().to_string();
        let html = response
            .text()
            .await
            .map_err(|error| auth_error(&format!("cannot read the gateway's reply: {error}")))?;

        match web_login::classify(&html) {
            Page::Done => {
                let Some(cookie) = cookie else {
                    // No form left to fill and no session: the gateway is
                    // showing something this code cannot act on — an error
                    // page, or a sign-in it wants done another way.
                    return Err(auth_error(
                        "the gateway ended the sign-in without a session cookie",
                    ));
                };
                let gwcert =
                    gwcert.ok_or_else(|| auth_error("could not pin the gateway's certificate"))?;
                return Ok(SignIn {
                    session: Session {
                        cookie: web_login::cookie_string(dialect.cookie, &cookie),
                        host: profile.gateway.clone(),
                        gwcert,
                    },
                    remember_password: credentials.and_then(|(_, _, remember)| remember),
                });
            }
            Page::Credentials(form) => {
                // Asked twice means the first answer was wrong; the stored
                // password is dropped by the caller on the error.
                if credentials.is_some() {
                    return Err(auth_error("the gateway rejected the credentials"));
                }
                let asked = ask(profile, state).await?;
                let fields = form.filled(&asked.0, &asked.1);
                body = Some(web_login::body(&fields));
                url = web_login::resolve_action(&page_url, form.action.as_deref());
                credentials = Some(asked);
            }
            Page::Interstitial(form) => {
                // A realm picker, a role choice, or "you already have a
                // session": answered by sending it back as it came.
                body = Some(web_login::body(&form.fields));
                url = web_login::resolve_action(&page_url, form.action.as_deref());
            }
        }
    }

    Err(auth_error("the gateway kept asking for more pages"))
}

/// `Set-Cookie` header values, as strings.
fn set_cookies(response: &reqwest::Response) -> Vec<String> {
    response
        .headers()
        .get_all(SET_COOKIE)
        .iter()
        .filter_map(|value| value.to_str().ok().map(str::to_owned))
        .collect()
}

/// Asks for whatever of the username and password is not already known.
///
/// Returns the username, the password, and the password to remember if the
/// user typed it.
async fn ask(
    profile: &Profile,
    state: &SecretAgentState,
) -> Result<(String, String, Option<String>), Error> {
    let username = profile.username.clone().unwrap_or_default();
    let stored = super::cache::password(&profile.uuid);

    if !username.is_empty()
        && let Some(password) = stored
    {
        return Ok((username, password, None));
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

    let password = values.get("password").cloned().unwrap_or_default();
    Ok((
        values.get("username").cloned().unwrap_or(username),
        password.clone(),
        Some(password).filter(|value| !value.is_empty()),
    ))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_protocol_knows_its_endpoint_and_cookie() {
        // Juniper and Pulse share the dana-na front end; F5 is its own.
        assert_eq!(dialect("nc"), Some(JUNIPER));
        assert_eq!(dialect("pulse"), Some(JUNIPER));
        assert_eq!(dialect("f5"), Some(F5));

        assert_eq!(JUNIPER.cookie, "DSID");
        assert_eq!(F5.cookie, "MRHSession");
        assert!(JUNIPER.login_path.starts_with('/'));
        assert!(F5.login_path.starts_with('/'));
    }

    #[test]
    fn the_protocols_this_module_does_not_speak_are_left_alone() {
        // These have their own native sign-ins; routing them here would
        // replace a working one with a form scrape.
        for protocol in ["gp", "anyconnect", "fortinet", "array", ""] {
            assert_eq!(dialect(protocol), None, "{protocol} was claimed");
        }
    }

    #[test]
    fn the_cookie_the_plugin_gets_is_named_for_the_protocol() {
        assert_eq!(web_login::cookie_string(JUNIPER.cookie, "abc"), "DSID=abc");
        assert_eq!(web_login::cookie_string(F5.cookie, "xyz"), "MRHSession=xyz");
    }
}
