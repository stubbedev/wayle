//! GlobalProtect gateway authentication, spoken natively over HTTPS.
//!
//! Authenticating to a GlobalProtect gateway is a form POST that answers with
//! either a challenge (2FA) or a session cookie. None of it needs a VPN
//! client: the `openconnect` binary's `--authenticate` mode does exactly this
//! and then prints the cookie for someone else to use. wayle does the same
//! thing in-process and hands the result to NetworkManager's openconnect
//! plugin, which is what actually carries the packets.
//!
//! The wire details — endpoint, body fields, and above all the *positional*
//! meaning of the `<argument>` elements in the reply — are openconnect's
//! `auth-globalprotect.c`. They are pinned by the tests below, because a
//! silent shift in that list would hand the gateway's cookie slot to something
//! else and fail in a way no error message would explain.

use std::collections::HashMap;

use super::{form, xml};
use crate::Error;

/// The client version every GlobalProtect gateway expects, and the one it
/// echoes back for us to check.
const CLIENT_VERSION: &str = "4100";

/// Positional meaning of the `<argument>` list in a successful login reply.
///
/// Empty entries are real: the gateway sends placeholder slots, and collapsing
/// them would shift every later argument onto the wrong name.
const LOGIN_ARGS: &[&str] = &[
    "",
    "authcookie",
    "persistent-cookie",
    "portal",
    "user",
    "authentication-source",
    "configuration",
    "domain",
    "",
    "",
    "",
    "",
    "connection-type",
    "password-expiration-days",
    "clientVer",
    "preferred-ip",
    "portal-userauthcookie",
    "portal-prelogonuserauthcookie",
    "preferred-ipv6",
    "usually-equals-4",
    "usually-equals-unknown",
];

/// The arguments that go into the cookie, in the order openconnect writes
/// them. `computer` is appended after these, from the local hostname.
const COOKIE_ARGS: &[&str] = &[
    "authcookie",
    "portal",
    "user",
    "domain",
    "preferred-ip",
    "preferred-ipv6",
];

/// What a login attempt produced.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) enum Step {
    /// Authentication succeeded; this is the cookie the plugin needs.
    Authenticated(Session),
    /// The gateway wants a second factor.
    Challenge {
        /// The gateway's own wording of what it wants.
        prompt: String,
        /// Opaque token identifying this challenge, echoed on the next post.
        input_str: String,
    },
}

/// What a gateway says about itself before anyone signs in.
///
/// Worth asking for: the labels and the message are the administrator's own
/// wording for what this gateway wants, and a SAML gateway can be recognised
/// here — before any credentials are posted at it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct Prelogin {
    /// The gateway's instruction to the user, when it sent one.
    pub message: Option<String>,
    /// What it calls the username field.
    pub username_label: String,
    /// What it calls the password field.
    pub password_label: String,
}

impl Default for Prelogin {
    fn default() -> Self {
        Self {
            message: None,
            username_label: String::from("Username"),
            password_label: String::from("Password"),
        }
    }
}

/// A completed authentication.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct Session {
    /// The `--cookie` string openconnect takes: `authcookie=…&portal=…&…`.
    pub cookie: String,
    /// The host the cookie was issued for. Handed back as the `gateway`
    /// secret, so the plugin connects to the same one that authenticated.
    pub host: String,
}

/// Builds a login request body.
///
/// The base fields are openconnect's verbatim; the empty ones are sent because
/// openconnect sends them, and a gateway that has only ever seen openconnect is
/// not the place to find out which of them are optional.
fn login_body(
    gateway: &str,
    user: &str,
    password: &str,
    computer: &str,
    input_str: &str,
) -> String {
    form::encode(&[
        ("jnlpReady", "jnlpReady"),
        ("ok", "Login"),
        ("direct", "yes"),
        ("clientVer", CLIENT_VERSION),
        ("prot", "https:"),
        ("internal", "no"),
        ("ipv6-support", "yes"),
        ("clientos", "Linux"),
        ("os-version", "Linux"),
        ("server", gateway),
        ("computer", computer),
        ("portal-userauthcookie", ""),
        ("portal-prelogonuserauthcookie", ""),
        ("preferred-ip", ""),
        ("preferred-ipv6", ""),
        ("inputStr", input_str),
        ("user", user),
        ("passwd", password),
    ])
}

/// Reads a gateway's answer to a login post.
///
/// # Errors
///
/// Returns an error when the gateway reported one, when it demanded SAML —
/// which needs a browser wayle deliberately does not embed — or when the reply
/// is not a shape this code knows.
fn parse_login(body: &str, gateway: &str, computer: &str) -> Result<Step, Error> {
    if let Some(input_str) = xml::value(body, "inputstr") {
        let prompt = xml::value(body, "respmsg").unwrap_or_default();
        return Ok(Step::Challenge { prompt, input_str });
    }

    let arguments = xml::values(body, "argument");
    if arguments.is_empty() {
        // ponytail: SAML portals need an embedded webview to catch the
        // returned cookie; say so plainly rather than failing as "malformed".
        if xml::value(body, "saml-auth-method").is_some() || body.contains("saml-request") {
            return Err(auth_error(
                "this gateway requires SAML sign-in, which wayle cannot do yet",
            ));
        }
        let message = xml::value(body, "msg")
            .filter(|message| !message.is_empty())
            .unwrap_or_else(|| String::from("the gateway rejected the login"));
        return Err(auth_error(&message));
    }

    let named = name_arguments(&arguments);
    check(&named, "connection-type", "tunnel")?;
    check(&named, "clientVer", CLIENT_VERSION)?;

    let authcookie = named
        .get("authcookie")
        .filter(|value| !value.is_empty())
        .ok_or_else(|| auth_error("the gateway returned no session cookie"))?;
    if named.get("user").is_none_or(String::is_empty) {
        return Err(auth_error("the gateway returned no user"));
    }
    debug_assert!(!authcookie.is_empty());

    Ok(Step::Authenticated(Session {
        cookie: build_cookie(&named, computer),
        host: String::from(gateway),
    }))
}

/// Pairs the positional arguments with their meanings, dropping the
/// placeholder slots and anything past the end of the known list.
fn name_arguments(arguments: &[String]) -> HashMap<String, String> {
    arguments
        .iter()
        .zip(LOGIN_ARGS)
        .filter(|(_, name)| !name.is_empty())
        .map(|(value, name)| ((*name).to_owned(), value.clone()))
        .collect()
}

/// A field the gateway is expected to echo back unchanged. A mismatch means
/// the reply is not the one this code knows how to read, and continuing would
/// build a cookie out of the wrong slots.
fn check(named: &HashMap<String, String>, key: &str, expected: &str) -> Result<(), Error> {
    match named.get(key) {
        Some(value) if value == expected => Ok(()),
        Some(value) => Err(auth_error(&format!(
            "gateway returned {key}={value}, expected {expected}"
        ))),
        None => Err(auth_error(&format!("gateway returned no {key}"))),
    }
}

/// Assembles the `--cookie` string openconnect's GlobalProtect support takes.
fn build_cookie(named: &HashMap<String, String>, computer: &str) -> String {
    let mut pairs: Vec<(&str, &str)> = COOKIE_ARGS
        .iter()
        .filter_map(|key| named.get(*key).map(|value| (*key, value.as_str())))
        .collect();
    pairs.push(("computer", computer));
    form::encode(&pairs)
}

fn auth_error(message: &str) -> Error {
    Error::VpnAuthenticationFailed(String::from(message))
}

/// Reads a gateway's prelogin response.
///
/// # Errors
///
/// Returns an error when the gateway reports one, or when it wants SAML.
fn parse_prelogin(body: &str) -> Result<Prelogin, Error> {
    // Checked before the status, because a SAML gateway answers `Success`:
    // from its point of view nothing is wrong, it just wants a browser.
    if xml::value(body, "saml-auth-method").is_some_and(|method| !method.is_empty())
        || xml::value(body, "saml-request").is_some_and(|request| !request.is_empty())
    {
        return Err(auth_error(
            "this gateway requires SAML sign-in, which wayle cannot do yet",
        ));
    }

    if xml::value(body, "status").is_some_and(|status| !status.eq_ignore_ascii_case("success")) {
        let message = xml::value(body, "msg")
            .filter(|message| !message.is_empty())
            .unwrap_or_else(|| String::from("the gateway refused the request"));
        return Err(auth_error(&message));
    }

    let default = Prelogin::default();
    Ok(Prelogin {
        message: xml::value(body, "authentication-message").filter(|m| !m.is_empty()),
        username_label: xml::value(body, "username-label")
            .filter(|label| !label.is_empty())
            .unwrap_or(default.username_label),
        password_label: xml::value(body, "password-label")
            .filter(|label| !label.is_empty())
            .unwrap_or(default.password_label),
    })
}

/// Asks a gateway what it wants, before anyone types anything.
///
/// # Errors
///
/// Returns an error when the gateway is unreachable, is not a GlobalProtect
/// gateway, or wants an authentication method wayle does not implement.
pub(super) async fn prelogin(client: &reqwest::Client, gateway: &str) -> Result<Prelogin, Error> {
    let url = format!(
        "https://{gateway}/ssl-vpn/prelogin.esp?tmp=tmp&clientVer={CLIENT_VERSION}&clientos=Linux"
    );
    let response = client.get(&url).send().await.map_err(|error| {
        Error::VpnAuthenticationFailed(format!("cannot reach the gateway: {error}"))
    })?;

    if response.status() == reqwest::StatusCode::NOT_FOUND {
        return Err(auth_error(
            "no GlobalProtect gateway at this address (a portal address needs its gateway's name)",
        ));
    }
    let body = response.text().await.map_err(|error| {
        Error::VpnAuthenticationFailed(format!("cannot read the gateway's reply: {error}"))
    })?;

    parse_prelogin(&body)
}

/// Posts a login (or a challenge answer) to a gateway.
///
/// # Errors
///
/// Returns an error when the request cannot be made, or when the gateway's
/// answer is a refusal rather than a cookie or a challenge.
pub(super) async fn login(
    client: &reqwest::Client,
    gateway: &str,
    user: &str,
    password: &str,
    computer: &str,
    input_str: &str,
) -> Result<Step, Error> {
    let url = format!("https://{gateway}/ssl-vpn/login.esp");
    let body = login_body(gateway, user, password, computer, input_str);

    let response = client
        .post(&url)
        .header(
            reqwest::header::CONTENT_TYPE,
            "application/x-www-form-urlencoded",
        )
        .body(body)
        .send()
        .await
        .map_err(|error| {
            Error::VpnAuthenticationFailed(format!("cannot reach the gateway: {error}"))
        })?;

    // A gateway answers a bad password with 200 and an error document, so the
    // status is only interesting when it is a real transport failure — most
    // usefully 404, which is what a portal-only host says to a gateway login.
    let status = response.status();
    let body = response.text().await.map_err(|error| {
        Error::VpnAuthenticationFailed(format!("cannot read the gateway's reply: {error}"))
    })?;

    if status == reqwest::StatusCode::NOT_FOUND {
        return Err(auth_error(
            "no GlobalProtect gateway at this address (a portal address needs its gateway's name)",
        ));
    }

    parse_login(&body, gateway, computer)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A real gateway's reply, trimmed to the argument list.
    fn success_xml(connection_type: &str, client_version: &str) -> String {
        format!(
            "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\
             <jnlp><application-desc>\
             <argument/>\
             <argument>AUTHCOOKIEVALUE</argument>\
             <argument>0123456789abcdef</argument>\
             <argument>vpn.example.com</argument>\
             <argument>alice</argument>\
             <argument>LDAP-auth</argument>\
             <argument>vsys1</argument>\
             <argument>example</argument>\
             <argument/><argument/><argument/><argument/>\
             <argument>{connection_type}</argument>\
             <argument>-1</argument>\
             <argument>{client_version}</argument>\
             <argument/>\
             <argument>PORTALCOOKIE</argument>\
             <argument/>\
             <argument/>\
             <argument>4</argument>\
             <argument>unknown</argument>\
             </application-desc></jnlp>"
        )
    }

    #[test]
    fn a_successful_login_becomes_openconnects_cookie_string() {
        let step = parse_login(&success_xml("tunnel", "4100"), "vpn.example.com", "laptop")
            .expect("authenticated");
        let Step::Authenticated(session) = step else {
            panic!("expected an authenticated step");
        };
        assert_eq!(session.host, "vpn.example.com");
        assert_eq!(
            session.cookie,
            "authcookie=AUTHCOOKIEVALUE&portal=vpn.example.com&user=alice&domain=example\
             &preferred-ip=&preferred-ipv6=&computer=laptop"
        );
    }

    #[test]
    fn the_argument_list_is_positional_and_stays_that_way() {
        // The whole protocol hinges on this: argument 1 is the cookie and
        // argument 4 is the user. A shift would silently mint a broken cookie.
        assert_eq!(LOGIN_ARGS[1], "authcookie");
        assert_eq!(LOGIN_ARGS[4], "user");
        assert_eq!(LOGIN_ARGS[7], "domain");
        assert_eq!(LOGIN_ARGS[12], "connection-type");
        assert_eq!(LOGIN_ARGS[14], "clientVer");
        assert_eq!(LOGIN_ARGS.len(), 21);

        let named = name_arguments(&xml::values(&success_xml("tunnel", "4100"), "argument"));
        assert_eq!(
            named.get("authcookie").map(String::as_str),
            Some("AUTHCOOKIEVALUE")
        );
        assert_eq!(named.get("user").map(String::as_str), Some("alice"));
        // The placeholder slots carry no name and so contribute nothing.
        assert!(!named.contains_key(""));
    }

    #[test]
    fn a_reply_that_is_not_a_tunnel_is_refused_rather_than_used() {
        let error = parse_login(
            &success_xml("not-a-tunnel", "4100"),
            "vpn.example.com",
            "laptop",
        )
        .expect_err("must not accept a non-tunnel reply");
        assert!(
            error.to_string().contains("connection-type"),
            "the error must name the field that disagreed: {error}"
        );
    }

    #[test]
    fn an_unexpected_client_version_is_refused() {
        // A gateway echoing a different version is answering some other
        // protocol; its argument list cannot be trusted to be this one.
        assert!(parse_login(&success_xml("tunnel", "5000"), "vpn.example.com", "laptop").is_err());
    }

    #[test]
    fn a_challenge_is_recognised_with_its_own_wording() {
        let xml = "<challenge><respmsg>Enter your token code</respmsg>\
                   <inputstr>CHALLENGE-1</inputstr></challenge>";
        let step = parse_login(xml, "vpn.example.com", "laptop").expect("challenge");
        assert_eq!(
            step,
            Step::Challenge {
                prompt: String::from("Enter your token code"),
                input_str: String::from("CHALLENGE-1"),
            }
        );
    }

    #[test]
    fn a_challenge_is_not_mistaken_for_a_success() {
        let xml = "<challenge><inputstr>C</inputstr></challenge>";
        assert!(!matches!(
            parse_login(xml, "vpn.example.com", "laptop"),
            Ok(Step::Authenticated(_))
        ));
    }

    #[test]
    fn a_rejection_surfaces_the_gateways_own_message() {
        let xml = "<response status=\"error\"><msg>Invalid username or password</msg></response>";
        let error = parse_login(xml, "vpn.example.com", "laptop").expect_err("rejected");
        assert!(
            error.to_string().contains("Invalid username or password"),
            "got: {error}"
        );
    }

    /// The real response from a form-authenticating gateway.
    const PRELOGIN_FORM: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\" ?>\
        <prelogin-response><status>Success</status><ccusername></ccusername>\
        <autosubmit>false</autosubmit><msg></msg><newmsg></newmsg><license>yes</license>\
        <authentication-message>Enter login credentials</authentication-message>\
        <username-label>Username</username-label><password-label>Password</password-label>\
        <panos-version>2</panos-version><saml-default-browser>yes</saml-default-browser>\
        <auth-api>no</auth-api><region>DK</region></prelogin-response>";

    #[test]
    fn a_form_gateway_hands_back_its_own_labels_and_message() {
        let prelogin = parse_prelogin(PRELOGIN_FORM).expect("a form gateway is usable");
        assert_eq!(prelogin.message.as_deref(), Some("Enter login credentials"));
        assert_eq!(prelogin.username_label, "Username");
        assert_eq!(prelogin.password_label, "Password");
    }

    #[test]
    fn advertising_a_saml_browser_is_not_the_same_as_requiring_saml() {
        // A form gateway still reports `saml-default-browser`; treating that
        // as "requires SAML" would refuse a gateway that works fine.
        assert!(PRELOGIN_FORM.contains("saml-default-browser"));
        assert!(parse_prelogin(PRELOGIN_FORM).is_ok());
    }

    #[test]
    fn a_saml_gateway_is_refused_before_any_credentials_are_posted_at_it() {
        let saml = "<prelogin-response><status>Success</status>\
            <saml-auth-method>REDIRECT</saml-auth-method>\
            <saml-request>aHR0cHM6Ly9pZHA=</saml-request></prelogin-response>";
        let error = parse_prelogin(saml).expect_err("saml is refused");
        assert!(error.to_string().contains("SAML"), "got: {error}");
    }

    #[test]
    fn a_prelogin_error_surfaces_the_gateways_message() {
        let refused = "<prelogin-response><status>Error</status>\
            <msg>Portal not licensed</msg></prelogin-response>";
        let error = parse_prelogin(refused).expect_err("an error status is an error");
        assert!(
            error.to_string().contains("Portal not licensed"),
            "got: {error}"
        );
    }

    #[test]
    fn a_gateway_that_sends_no_labels_still_produces_a_usable_prompt() {
        let bare = "<prelogin-response><status>Success</status></prelogin-response>";
        let prelogin = parse_prelogin(bare).expect("usable");
        assert_eq!(prelogin.username_label, "Username");
        assert_eq!(prelogin.password_label, "Password");
        assert_eq!(prelogin.message, None);
    }

    #[test]
    fn a_saml_portal_says_so_instead_of_reading_as_malformed() {
        let xml = "<prelogin-response><saml-auth-method>REDIRECT</saml-auth-method>\
                   <saml-request>aHR0cHM6Ly9pZHA=</saml-request></prelogin-response>";
        let error = parse_login(xml, "vpn.example.com", "laptop").expect_err("saml");
        assert!(error.to_string().contains("SAML"), "got: {error}");
    }

    #[test]
    fn a_login_without_a_cookie_is_a_failure_not_an_empty_success() {
        let xml = success_xml("tunnel", "4100").replace("AUTHCOOKIEVALUE", "");
        assert!(parse_login(&xml, "vpn.example.com", "laptop").is_err());
    }

    #[test]
    fn the_first_post_carries_an_empty_challenge_field() {
        let body = login_body("vpn.example.com", "alice", "pw", "laptop", "");
        assert!(body.contains("&inputStr=&"), "got: {body}");
        assert!(body.contains("clientVer=4100"));
        assert!(body.contains("&user=alice&passwd=pw"));
    }

    #[test]
    fn a_challenge_answer_carries_the_token_it_answers() {
        let body = login_body(
            "vpn.example.com",
            "alice",
            "123456",
            "laptop",
            "CHALLENGE-1",
        );
        assert!(body.contains("inputStr=CHALLENGE-1"), "got: {body}");
        assert!(body.contains("passwd=123456"));
    }

    /// Talks to a real gateway. Ignored by default and driven by an
    /// environment variable, because the hostname is site-specific and the
    /// repository is not the place for someone's VPN address:
    ///
    /// ```sh
    /// WAYLE_TEST_GP_GATEWAY=vpn.example.com \
    ///   cargo test -p wayle-network --lib -- --ignored a_real_gateway
    /// ```
    ///
    /// Unauthenticated: prelogin is what any GlobalProtect client asks before
    /// showing a login box, so this posts no credentials at anything.
    #[tokio::test]
    #[ignore = "needs WAYLE_TEST_GP_GATEWAY and network access to it"]
    async fn a_real_gateway_answers_prelogin_over_system_trust() {
        let Ok(gateway) = std::env::var("WAYLE_TEST_GP_GATEWAY") else {
            panic!("set WAYLE_TEST_GP_GATEWAY to the gateway to probe");
        };
        let client = super::super::client().expect("client builds");

        let prelogin = prelogin(&client, &gateway)
            .await
            .expect("the gateway answers prelogin, and its certificate validates");

        // Labels always come back populated, from the gateway or the default.
        assert!(!prelogin.username_label.is_empty());
        assert!(!prelogin.password_label.is_empty());
    }

    #[test]
    fn credentials_needing_escaping_survive_the_round_trip() {
        let body = login_body(
            "vpn.example.com",
            "EXAMPLE\\alice",
            "p@ss&word",
            "laptop",
            "",
        );
        assert!(body.contains("user=EXAMPLE%5Calice"), "got: {body}");
        assert!(body.contains("passwd=p%40ss%26word"), "got: {body}");
    }
}
