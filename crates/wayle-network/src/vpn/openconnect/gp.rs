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

use super::{LOGIN_TIMEOUT, PRELOGIN_TIMEOUT, Session, form, peer_pin, xml};
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
    /// The browser sign-in this gateway wants, when it wants one instead of a
    /// username and a password.
    pub saml: Option<super::gp_sso::SamlRequest>,
    /// What it calls the username field.
    pub username_label: String,
    /// What it calls the password field.
    pub password_label: String,
}

impl Default for Prelogin {
    fn default() -> Self {
        Self {
            message: None,
            saml: None,
            username_label: String::from("Username"),
            password_label: String::from("Password"),
        }
    }
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

/// The custom status a GlobalProtect gateway answers a bad username or
/// password with. openconnect's `do_https_request` maps it to `-EACCES`, the
/// one refusal it treats as "ask for the password again".
const STATUS_BAD_CREDENTIALS: u16 = 512;

/// The header PAN-OS names its reason for a refusal in, next to whatever the
/// body says.
const REASON_HEADER: &str = "x-private-pan-globalprotect";

/// A login reply as it came off the wire: the parts a refusal can hide its
/// reason in, besides the body.
#[derive(Debug, Clone, Copy)]
struct Reply<'a> {
    status: u16,
    /// The [`REASON_HEADER`] value, when the gateway sent one.
    reason: Option<&'a str>,
    body: &'a str,
}

impl<'a> Reply<'a> {
    /// A 200 reply with this body and no reason header, which is how a gateway
    /// answers everything that is not a bad password.
    #[cfg(test)]
    fn ok(body: &'a str) -> Self {
        Self {
            status: 200,
            reason: None,
            body,
        }
    }
}

/// What the JavaScript-shaped reply some gateways write says.
///
/// openconnect's `parse_javascript`: GlobalProtect gateways send challenges,
/// and refusals, as three lines of script instead of XML, sometimes wrapped in
/// an HTML page, with no warning which shape is coming.
#[derive(Debug, Clone, PartialEq, Eq)]
enum Script {
    Challenge { prompt: String, input_str: String },
    Error(String),
}

/// Reads `var respStatus = "…"; var respMsg = "…"; thisForm.inputStr.value =
/// "…";` out of a reply, wherever in it the script sits.
///
/// An `Error` needs no `inputStr`: it has nothing to be echoed back, and a
/// refusal is worth reading even from a gateway that leaves the line off. A
/// `Challenge` without one cannot be answered, so it is not one.
fn parse_script(body: &str) -> Option<Script> {
    const STATUS: &str = "var respStatus = \"";
    const MESSAGE: &str = "var respMsg = \"";
    const INPUT_STR: &str = "thisForm.inputStr.value = \"";

    let rest = &body[body.find(STATUS)? + STATUS.len()..];
    let (status, rest) = rest.split_once('"')?;
    let rest = &rest[rest.find(MESSAGE)? + MESSAGE.len()..];
    let (message, rest) = script_string(rest)?;

    if status.starts_with("Error") {
        return Some(Script::Error(message));
    }
    if !status.starts_with("Challenge") {
        return None;
    }
    let rest = &rest[rest.find(INPUT_STR)? + INPUT_STR.len()..];
    let (input_str, _) = rest.split_once('"')?;
    Some(Script::Challenge {
        prompt: message,
        input_str: String::from(input_str),
    })
}

/// The contents of a double-quoted script string, unescaped, and what follows
/// its closing quote.
fn script_string(raw: &str) -> Option<(String, &str)> {
    let mut out = String::new();
    let mut chars = raw.char_indices();
    while let Some((at, character)) = chars.next() {
        match character {
            '"' => return Some((out, &raw[at + 1..])),
            '\\' => match chars.next()?.1 {
                'n' => out.push('\n'),
                't' => out.push('\t'),
                other => out.push(other),
            },
            other => out.push(other),
        }
    }
    None
}

/// Why the gateway refused, in the best words the reply has.
///
/// The body's own message first, in whichever of its shapes it came — the
/// `<error>` of a `<response status="error">`, a prelogin-style `<msg>`, or
/// the script's `respMsg` — because that is the administrator's wording. A
/// bare 512 is openconnect's "invalid username or password" even with nothing
/// written around it. Past that, PAN's reason header, and last the status, so
/// a refusal nobody explained still says *something* a person can search for.
fn refusal(reply: Reply<'_>) -> Error {
    let from_body = match parse_script(reply.body) {
        Some(Script::Error(message)) => Some(message),
        _ => xml::values(reply.body, "error")
            .into_iter()
            .chain(xml::value(reply.body, "msg"))
            .find(|message| !message.trim().is_empty()),
    };
    if let Some(message) = from_body {
        return auth_error(message.trim());
    }
    if reply.status == STATUS_BAD_CREDENTIALS {
        return auth_error("wrong username or password");
    }
    if let Some(reason) = reply
        .reason
        .map(str::trim)
        .filter(|reason| !reason.is_empty())
    {
        return auth_error(&format!("the gateway refused the sign-in ({reason})"));
    }
    auth_error(&format!(
        "the gateway refused the sign-in without saying why (HTTP {})",
        reply.status
    ))
}

/// Reads a gateway's answer to a login post.
///
/// # Errors
///
/// Returns an error when the gateway reported one, when it demanded SAML —
/// which needs a browser wayle deliberately does not embed — or when the reply
/// is not a shape this code knows.
fn parse_login(
    reply: Reply<'_>,
    gateway: &str,
    computer: &str,
    gwcert: &str,
) -> Result<Step, Error> {
    let body = reply.body;
    match parse_script(body) {
        Some(Script::Challenge { prompt, input_str }) => {
            return Ok(Step::Challenge { prompt, input_str });
        }
        Some(Script::Error(_)) => return Err(refusal(reply)),
        None => {}
    }

    // A 512 is a refusal whatever its body looks like; nothing in it is a
    // cookie or a challenge to be answered.
    if reply.status == STATUS_BAD_CREDENTIALS {
        return Err(refusal(reply));
    }

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
        return Err(refusal(reply));
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
        gwcert: String::from(gwcert),
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
    // Read before the status, because a SAML gateway answers `Success`: from
    // its point of view nothing is wrong, it just wants a browser. Whether
    // wayle can act on that is the caller's decision — the profile has to have
    // asked for the browser sign-in.
    let saml = super::gp_sso::parse_request(body);

    if xml::value(body, "status").is_some_and(|status| !status.eq_ignore_ascii_case("success")) {
        let message = xml::value(body, "msg")
            .filter(|message| !message.is_empty())
            .unwrap_or_else(|| String::from("the gateway refused the request"));
        return Err(auth_error(&message));
    }

    let default = Prelogin::default();
    Ok(Prelogin {
        message: xml::value(body, "authentication-message").filter(|m| !m.is_empty()),
        saml,
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
    let response = client
        .get(&url)
        .timeout(PRELOGIN_TIMEOUT)
        .send()
        .await
        .map_err(|error| {
            Error::VpnSignInIncomplete(format!("cannot reach the gateway: {error}"))
        })?;

    if response.status() == reqwest::StatusCode::NOT_FOUND {
        return Err(auth_error(
            "no GlobalProtect gateway at this address (a portal address needs its gateway's name)",
        ));
    }
    let body = response.text().await.map_err(|error| {
        Error::VpnSignInIncomplete(format!("cannot read the gateway's reply: {error}"))
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
        // A gateway doing push MFA holds this open until the user has tapped
        // approve on their phone, so this is the one request that waits on a
        // person rather than on a server.
        .timeout(LOGIN_TIMEOUT)
        .body(body)
        .send()
        .await
        .map_err(|error| {
            Error::VpnSignInIncomplete(format!("cannot reach the gateway: {error}"))
        })?;

    // Read off the response rather than a second connection: this is the
    // certificate that was in front of the gateway while it minted the cookie,
    // which is exactly what the plugin is asked to pin.
    let gwcert = peer_pin(&response).ok_or_else(|| {
        auth_error("cannot read the gateway's certificate, which the VPN plugin requires")
    })?;

    // A gateway answers a bad password with 200 and an error document, so the
    // status is only interesting when it is a real transport failure — most
    // usefully 404, which is what a portal-only host says to a gateway login.
    let status = response.status();
    let reason = response
        .headers()
        .get(REASON_HEADER)
        .and_then(|value| value.to_str().ok())
        .map(String::from);
    let body = response.text().await.map_err(|error| {
        Error::VpnSignInIncomplete(format!("cannot read the gateway's reply: {error}"))
    })?;

    if status == reqwest::StatusCode::NOT_FOUND {
        return Err(auth_error(
            "no GlobalProtect gateway at this address (a portal address needs its gateway's name)",
        ));
    }

    let reply = Reply {
        status: status.as_u16(),
        reason: reason.as_deref(),
        body: &body,
    };
    parse_login(reply, gateway, computer, &gwcert)
}

/// What a gateway says when asked to use a cookie again.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) enum Verdict {
    /// It answered with tunnel configuration: the cookie is still good.
    Accepts,
    /// It refused the cookie outright.
    Refuses,
    /// The reply says nothing usable about the cookie. Keeping it and letting
    /// the plugin's own attempt be the judge beats re-authenticating on the
    /// strength of a reply nobody understands.
    Unreadable,
}

/// Asks the gateway whether it still accepts a session cookie.
///
/// This is the request the plugin itself starts every tunnel with —
/// openconnect's `gpst_get_config` posting the cookie string to
/// `getconfig.esp` — so a gateway willing to bring the tunnel up with this
/// cookie says so, and one that has expired the session says so before
/// anything is handed to NM. A good cookie is answered with configuration; a
/// dead one with `<response status="error">`, and sometimes HTTP 512 for
/// good measure. openconnect's `gpst_xml_or_error` keys on exactly those.
///
/// # Errors
///
/// Only when the gateway cannot be reached at all. The caller treats that the
/// same as [`Verdict::Unreadable`]: a gateway nobody can reach cannot tell
/// anyone the cookie is bad.
pub(super) async fn cookie_verdict(
    client: &reqwest::Client,
    gateway: &str,
    cookie: &str,
) -> Result<Verdict, Error> {
    let url = format!("https://{gateway}/ssl-vpn/getconfig.esp");
    // The fields before the cookie are openconnect's own, verbatim; the
    // cookie string is appended whole, exactly as openconnect sends it.
    let body = format!(
        "client-type=1&protocol-version=p1&app-version=5.1.5-8&clientos=Linux&os-version=linux\
         &hmac-algo=sha1,md5,sha256&enc-algo=aes-128-cbc,aes-256-cbc&{cookie}"
    );
    let response = client
        .post(&url)
        .header(
            reqwest::header::CONTENT_TYPE,
            "application/x-www-form-urlencoded",
        )
        .timeout(PRELOGIN_TIMEOUT)
        .body(body)
        .send()
        .await
        .map_err(|error| {
            Error::VpnSignInIncomplete(format!("cannot reach the gateway: {error}"))
        })?;

    let status = response.status().as_u16();
    let body = response.text().await.map_err(|error| {
        Error::VpnSignInIncomplete(format!("cannot read the gateway's reply: {error}"))
    })?;
    Ok(verdict_from(status, &body))
}

fn verdict_from(status: u16, body: &str) -> Verdict {
    if refused(status, body) {
        return Verdict::Refuses;
    }
    match xml::raw_element(body, "response") {
        Some(_) => Verdict::Accepts,
        None => Verdict::Unreadable,
    }
}

fn refused(status: u16, body: &str) -> bool {
    // The custom status a gateway answers a dead cookie with, next to the
    // error document it writes around the same message.
    if status == 512 {
        return true;
    }
    if xml::values(body, "error")
        .iter()
        .any(|text| !text.is_empty())
    {
        return true;
    }
    xml::raw_element(body, "response").is_some_and(|response| {
        xml::attribute(response, "status").is_some_and(|value| value.eq_ignore_ascii_case("error"))
    })
}

#[cfg(test)]
// One test sets `SSL_CERT_FILE` so the mock gateway's committed certificate
// verifies through the same path a real one does.
#[allow(unsafe_code)]
mod tests {
    use super::*;

    /// Stands in for the pin `login` reads off the TLS connection.
    const PIN: &str = "pin-sha256:AAAA";

    mod verdict {
        use super::super::{Verdict, verdict_from};

        const CONFIG: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\" ?>\
            <response status=\"success\"><ip-address>192.168.241.222</ip-address>\
            <netmask>255.255.255.255</netmask><mtu>0</mtu><lifetime>86400</lifetime>\
            <access-routes><member>0.0.0.0/0</member></access-routes></response>";

        const DEAD_COOKIE: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\" ?>\
            <response status=\"error\">\
            <error>Invalid authentication cookie</error></response>";

        #[test]
        fn a_config_reply_means_the_cookie_still_opens_a_tunnel() {
            assert_eq!(verdict_from(200, CONFIG), Verdict::Accepts);
        }

        #[test]
        fn an_error_document_is_a_refused_cookie() {
            assert_eq!(verdict_from(200, DEAD_COOKIE), Verdict::Refuses);
        }

        #[test]
        fn the_gateways_custom_dead_cookie_status_refuses_without_xml() {
            // Some gateways answer the same refusal as an HTTP 512 with a
            // body that is not the error document at all.
            assert_eq!(verdict_from(512, "nonsense"), Verdict::Refuses);
        }

        #[test]
        fn a_reply_that_says_nothing_about_the_cookie_is_kept_anyway() {
            // An HTML error page or any other reply the reader cannot place
            // must not cost a cookie that might be fine.
            assert_eq!(
                verdict_from(200, "<html>something else</html>"),
                Verdict::Unreadable
            );
        }
    }

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
        let step = parse_login(
            Reply::ok(&success_xml("tunnel", "4100")),
            "vpn.example.com",
            "laptop",
            PIN,
        )
        .expect("authenticated");
        let Step::Authenticated(session) = step else {
            panic!("expected an authenticated step");
        };
        assert_eq!(session.host, "vpn.example.com");
        // The plugin refuses to start without this, however good the cookie.
        assert_eq!(session.gwcert, PIN);
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
            Reply::ok(&success_xml("not-a-tunnel", "4100")),
            "vpn.example.com",
            "laptop",
            PIN,
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
        assert!(
            parse_login(
                Reply::ok(&success_xml("tunnel", "5000")),
                "vpn.example.com",
                "laptop",
                PIN
            )
            .is_err()
        );
    }

    #[test]
    fn a_challenge_is_recognised_with_its_own_wording() {
        let xml = "<challenge><respmsg>Enter your token code</respmsg>\
                   <inputstr>CHALLENGE-1</inputstr></challenge>";
        let step =
            parse_login(Reply::ok(xml), "vpn.example.com", "laptop", PIN).expect("challenge");
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
            parse_login(Reply::ok(xml), "vpn.example.com", "laptop", PIN),
            Ok(Step::Authenticated(_))
        ));
    }

    #[test]
    fn a_rejection_surfaces_the_gateways_own_message() {
        let xml = "<response status=\"error\"><msg>Invalid username or password</msg></response>";
        let error =
            parse_login(Reply::ok(xml), "vpn.example.com", "laptop", PIN).expect_err("rejected");
        assert!(
            error.to_string().contains("Invalid username or password"),
            "got: {error}"
        );
    }

    /// The reason a refused login is reported with.
    fn refused(reply: Reply<'_>) -> String {
        let error = parse_login(reply, "vpn.example.com", "laptop", PIN)
            .expect_err("the gateway refused this login");
        assert!(
            matches!(error, Error::VpnAuthenticationFailed(_)),
            "a refusal must discredit the password: {error:?}"
        );
        error.to_string()
    }

    #[test]
    fn an_error_document_surfaces_its_error_element() {
        // openconnect's `<response status="error"><error>…</error>` shape,
        // which the old reader skipped for want of a `<msg>`.
        let xml = "<?xml version=\"1.0\" encoding=\"UTF-8\" ?>\
            <response status=\"error\">\
            <error>Authentication failed: Invalid username or password</error></response>";
        assert_eq!(
            refused(Reply::ok(xml)),
            "Authentication failed: Invalid username or password"
        );
    }

    const SCRIPT_ERROR: &str = "var respStatus = \"Error\";\n\
        var respMsg = \"Authentication failed: Invalid username or password \";\n\
        thisForm.inputStr.value = \"\";\n";

    #[test]
    fn a_script_shaped_refusal_surfaces_its_message() {
        assert_eq!(
            refused(Reply::ok(SCRIPT_ERROR)),
            "Authentication failed: Invalid username or password"
        );
    }

    #[test]
    fn a_script_refusal_wrapped_in_html_is_still_read() {
        let html = format!("<html><head></head><body>{SCRIPT_ERROR}</body></html>");
        assert_eq!(
            refused(Reply::ok(&html)),
            "Authentication failed: Invalid username or password"
        );
    }

    #[test]
    fn a_script_shaped_challenge_is_answered_like_an_xml_one() {
        let script = "var respStatus = \"Challenge\";\n\
            var respMsg = \"Enter the \\\"code\\\" from your phone\";\n\
            thisForm.inputStr.value = \"CHALLENGE-1\";\n";
        assert_eq!(
            parse_login(Reply::ok(script), "vpn.example.com", "laptop", PIN).expect("challenge"),
            Step::Challenge {
                prompt: String::from("Enter the \"code\" from your phone"),
                input_str: String::from("CHALLENGE-1"),
            }
        );
    }

    #[test]
    fn a_script_challenge_with_no_token_is_not_a_challenge() {
        // Nothing to echo back means nothing to answer: prompting for a code
        // the gateway could never match would waste the user's second factor.
        let script = "var respStatus = \"Challenge\";\nvar respMsg = \"Enter code\";\n";
        assert!(!matches!(
            parse_login(Reply::ok(script), "vpn.example.com", "laptop", PIN),
            Ok(Step::Challenge { .. })
        ));
    }

    #[test]
    fn a_bare_512_is_a_wrong_password() {
        let reply = Reply {
            status: STATUS_BAD_CREDENTIALS,
            reason: None,
            body: "",
        };
        assert_eq!(refused(reply), "wrong username or password");
    }

    #[test]
    fn a_512_is_a_refusal_whatever_its_body_looks_like() {
        let body = success_xml("tunnel", "4100");
        let reply = Reply {
            status: STATUS_BAD_CREDENTIALS,
            reason: None,
            body: &body,
        };
        assert_eq!(refused(reply), "wrong username or password");
    }

    #[test]
    fn the_bodys_wording_wins_over_the_status_and_the_header() {
        let reply = Reply {
            status: STATUS_BAD_CREDENTIALS,
            reason: Some("auth-failed"),
            body: SCRIPT_ERROR,
        };
        assert_eq!(
            refused(reply),
            "Authentication failed: Invalid username or password"
        );
    }

    #[test]
    fn pans_reason_header_explains_a_refusal_with_an_empty_body() {
        let reply = Reply {
            status: 200,
            reason: Some("auth-failed"),
            body: "<response status=\"error\"></response>",
        };
        assert_eq!(
            refused(reply),
            "the gateway refused the sign-in (auth-failed)"
        );
    }

    #[test]
    fn an_unexplained_refusal_names_its_status_instead_of_saying_nothing() {
        let reason = refused(Reply {
            status: 403,
            reason: None,
            body: "<html>denied</html>",
        });
        assert!(reason.contains("HTTP 403"), "got: {reason}");
        assert!(!reason.contains("rejected the login"), "got: {reason}");
    }

    #[test]
    fn blank_messages_are_skipped_for_ones_that_say_something() {
        let xml =
            "<response status=\"error\"><error>  </error><msg>Account locked</msg></response>";
        assert_eq!(refused(Reply::ok(xml)), "Account locked");
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
    fn a_saml_gateway_is_recognised_before_any_credentials_are_posted_at_it() {
        let saml = "<prelogin-response><status>Success</status>\
            <saml-auth-method>REDIRECT</saml-auth-method>\
            <saml-request>aHR0cHM6Ly9pZHA=</saml-request></prelogin-response>";

        let prelogin = parse_prelogin(saml).expect("a SAML gateway parses");

        // The request is carried out of parsing rather than refused here:
        // whether wayle signs in through the browser is the profile's choice,
        // and `credentials` is where that is known. What must not happen is
        // this reading as an ordinary username/password gateway.
        let request = prelogin.saml.expect("the SAML request");
        assert_eq!(request.payload, "https://idp");
        assert_eq!(request.method, super::super::gp_sso::Method::Redirect);
    }

    #[test]
    fn an_ordinary_gateway_carries_no_saml_request() {
        // The form gateway advertises `saml-default-browser` and is still a
        // form gateway; treating that as SAML would send it to the browser.
        assert!(PRELOGIN_FORM.contains("saml-default-browser"));
        assert!(
            parse_prelogin(PRELOGIN_FORM)
                .expect("a form gateway parses")
                .saml
                .is_none()
        );
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
        let error =
            parse_login(Reply::ok(xml), "vpn.example.com", "laptop", PIN).expect_err("saml");
        assert!(error.to_string().contains("SAML"), "got: {error}");
    }

    #[test]
    fn a_login_without_a_cookie_is_a_failure_not_an_empty_success() {
        let xml = success_xml("tunnel", "4100").replace("AUTHCOOKIEVALUE", "");
        assert!(parse_login(Reply::ok(&xml), "vpn.example.com", "laptop", PIN).is_err());
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

    /// Tests against the mock gateway in `tests/mock-gateway`, started by
    /// `just test-gateway`.
    ///
    /// These are the only tests that speak the protocol over a real TLS
    /// connection, which is the point: the `gwcert` secret is read off that
    /// connection, so no amount of parsing tests would have caught it missing.
    /// Nothing here touches anyone's real VPN — the certificate is committed
    /// beside the mock, which is what makes its pin a constant.
    mod mock {
        use super::*;

        const GATEWAY: &str = "127.0.0.1:8443";
        const SAML_GATEWAY: &str = "127.0.0.1:8444";
        const PIN: &str = "pin-sha256:eQO9gC6TVZtfFqt1YHSe7HUSxgHyRmhNo3UXeSAxvZI=";

        /// A client that trusts the mock's committed certificate.
        ///
        /// `SSL_CERT_FILE` rather than a client built differently for tests:
        /// this is the production client, verifying a gateway for real rather
        /// than being told to skip the check.
        fn client() -> reqwest::Client {
            // SAFETY: nextest runs every test in its own process.
            unsafe {
                std::env::set_var(
                    "SSL_CERT_FILE",
                    concat!(env!("CARGO_MANIFEST_DIR"), "/tests/mock-gateway/ca.crt"),
                );
            }
            super::super::super::client().expect("the client builds")
        }

        /// Signs in the way [`super::super::sign_in`] does: a password post,
        /// then the answer to the challenge it comes back with.
        async fn sign_in() -> Result<Session, Error> {
            let client = client();
            let step = login(&client, GATEWAY, "alice", "hunter2", "laptop", "").await?;
            let Step::Challenge { input_str, .. } = step else {
                panic!("the mock gateway always challenges once");
            };
            match login(&client, GATEWAY, "alice", "123456", "laptop", &input_str).await? {
                Step::Authenticated(session) => Ok(session),
                Step::Challenge { .. } => panic!("the second post completes the sign-in"),
            }
        }

        #[tokio::test]
        #[ignore = "needs the mock gateway: just test-gateway"]
        async fn prelogin_carries_the_gateways_own_wording() {
            let prelogin = prelogin(&client(), GATEWAY)
                .await
                .expect("the gateway answers, and its certificate validates");
            assert_eq!(prelogin.username_label, "Company ID");
            assert_eq!(prelogin.password_label, "Passphrase");
            assert_eq!(
                prelogin.message.as_deref(),
                Some("Sign in to the mock gateway")
            );
        }

        #[tokio::test]
        #[ignore = "needs the mock gateway: just test-gateway"]
        async fn a_sign_in_produces_a_cookie_and_the_certificate_pin() {
            let session = sign_in().await.expect("the mock gateway signs us in");

            assert!(
                session.cookie.starts_with("authcookie=AUTHCOOKIEVALUE&"),
                "got: {}",
                session.cookie
            );
            assert!(
                session.cookie.contains("&user=alice&"),
                "got: {}",
                session.cookie
            );
            assert_eq!(session.host, GATEWAY);
            // The whole of #12: without this the plugin never launches
            // openconnect, however good the cookie is. And it is the pin of
            // the certificate this connection actually presented, not one this
            // code could have invented for itself.
            assert_eq!(session.gwcert, PIN);
        }

        #[tokio::test]
        #[ignore = "needs the mock gateway: just test-gateway"]
        async fn the_gateway_accepts_the_cookie_it_minted() {
            let session = sign_in().await.expect("the mock gateway signs us in");
            assert_eq!(
                cookie_verdict(&client(), &session.host, &session.cookie)
                    .await
                    .expect("the gateway answers"),
                Verdict::Accepts,
                "a cookie the gateway just minted must not cost a sign-in"
            );
        }

        #[tokio::test]
        #[ignore = "needs the mock gateway: just test-gateway"]
        async fn the_gateway_refuses_a_cookie_it_did_not_mint() {
            let cookie =
                String::from("authcookie=EXPIRED&portal=127.0.0.1&user=alice&domain=example");
            assert_eq!(
                cookie_verdict(&client(), GATEWAY, &cookie)
                    .await
                    .expect("the gateway answers"),
                Verdict::Refuses,
                "a dead cookie must be caught before it is handed to NM"
            );
        }

        #[tokio::test]
        #[ignore = "needs the mock gateway: just test-gateway"]
        async fn a_wrong_password_is_a_refusal_in_the_gateways_words() {
            let error = login(&client(), GATEWAY, "alice", "wrong", "laptop", "")
                .await
                .expect_err("a bad password does not sign anyone in");
            assert!(
                error.to_string().contains("Invalid username or password"),
                "got: {error}"
            );
        }

        #[tokio::test]
        #[ignore = "needs the mock gateway: just test-gateway"]
        async fn a_saml_portal_is_reported_rather_than_signed_into() {
            // `prelogin` only reports what the gateway wants; whether to
            // refuse it is the caller's decision, because a profile may opt
            // into the browser sign-in. The refusal itself is tested against
            // `authenticate` in `mod.rs`, where the caller lives.
            let prelogin = prelogin(&client(), SAML_GATEWAY)
                .await
                .expect("a SAML portal answers its prelogin");
            let Some(request) = prelogin.saml else {
                panic!("the mock SAML portal advertises a browser sign-in");
            };
            assert_eq!(
                request.method,
                super::super::super::gp_sso::Method::Redirect,
                "the payload the callback scheme expects is a URL: {:?}",
                request.method
            );
        }
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
