//! AnyConnect (Cisco) gateway authentication, spoken natively over HTTPS.
//!
//! The same shape as [`super::gp`] and a different dialect. Where
//! GlobalProtect posts a fixed form and reads a positional argument list,
//! AnyConnect posts XML and is *told* what to ask: the gateway answers with a
//! `<form>` of `<input>` elements, and the reply carries one element per input
//! plus the `<opaque>` blob echoed back untouched. A second factor is the same
//! exchange again with a different form.
//!
//! The wire details are openconnect's `auth.c`. Where this cannot follow the
//! conversation it returns [`Error::VpnProtocolUnsupported`] rather than a
//! failure — that is NM's cue to let the plugin's own auth dialog try, so a
//! gateway this does not understand is no worse off than before wayle claimed
//! the protocol.

use super::{LOGIN_TIMEOUT, MAX_CHALLENGES, SSO_TIMEOUT, Session, SignIn, peer_pin, sso, xml};
use crate::{
    Error,
    agent::SecretAgentState,
    types::agent::{SecretField, SecretRequest},
    vpn::openconnect::Profile,
};

/// What openconnect reports itself as. Some gateways gate on the version.
const VERSION: &str = "v9.12";

/// The device identifier openconnect sends for a 64-bit Linux client.
const DEVICE_ID: &str = "linux-64";

/// The session cookie's name in the gateway's `Set-Cookie`. This value, as
/// `webvpn=…`, is what openconnect takes as its `--cookie`.
const COOKIE_NAME: &str = "webvpn";

/// One field the gateway is asking for.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Field {
    /// The element name the answer goes back under.
    name: String,
    /// What to call it on screen.
    label: String,
    /// Whether the answer must be hidden as it is typed.
    secret: bool,
    /// `<input type="sso">`: the value comes from the browser sign-in
    /// rather than from the user, so this field is never prompted for.
    sso: bool,
}

/// The form a gateway is asking to have filled in.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
struct Form {
    /// The gateway's own instruction, when it sent one.
    message: Option<String>,
    /// The fields to ask for, in the gateway's order.
    fields: Vec<Field>,
    /// The `<opaque>` blob to echo back verbatim, when there is one.
    opaque: Option<String>,
    /// The tunnel group to select, when the gateway offered a choice.
    group: Option<String>,
    /// `sso-v2-login`: the URL to open in the browser, when the gateway
    /// answered with the external-browser flow.
    sso_login: Option<String>,
}

/// What one exchange with the gateway produced.
#[derive(Debug, Clone, PartialEq, Eq)]
enum Step {
    /// Authenticated; the session token is in the cookie jar.
    Authenticated,
    /// The gateway wants this form filled in.
    Form(Box<Form>),
}

/// Signs in to an AnyConnect gateway, following however many forms it asks
/// for.
///
/// # Errors
///
/// Returns an error when the gateway refuses the credentials, when the user
/// dismisses a prompt, or — as [`Error::VpnProtocolUnsupported`] — when the
/// conversation is not one this understands.
pub(super) async fn sign_in(
    profile: &Profile,
    client: &reqwest::Client,
    state: &SecretAgentState,
) -> Result<SignIn, Error> {
    // The browser sign-in needs a key pair from the very first request: the
    // gateway encrypts the token to it. Generated only when the profile asked
    // for the flow, so an ordinary sign-in costs nothing.
    let mut keys = profile.sso.then(sso::Keys::generate).transpose()?;
    // Kept alongside: the header goes on every request of the conversation,
    // while the key itself is consumed the moment a token arrives.
    let pubkey = keys.as_ref().map(|keys| keys.public_base64.clone());
    let mut body = init_request(&profile.gateway, keys.is_some());
    let mut remember_password = None;

    for _ in 0..=MAX_CHALLENGES {
        let exchange = post(client, &profile.gateway, &body, pubkey.as_deref()).await?;
        let form = match exchange.step {
            Step::Authenticated => {
                let cookie = exchange.cookie.ok_or_else(|| {
                    auth_error("the gateway authenticated us but set no session cookie")
                })?;
                return Ok(SignIn {
                    session: Session {
                        cookie,
                        host: profile.gateway.clone(),
                        gwcert: exchange.gwcert,
                    },
                    remember_password,
                });
            }
            Step::Form(form) => *form,
        };

        let answers = answer(profile, &form, &mut keys, state).await?;
        // Only the first password is worth remembering: a second factor is a
        // second factor precisely because it is different every time.
        if remember_password.is_none() {
            remember_password = password_of(&form, &answers);
        }
        body = reply_request(&form, &answers);
    }

    Err(auth_error("the gateway kept asking for more factors"))
}

/// One round trip with the gateway.
struct Exchange {
    /// What the gateway wants next.
    step: Step,
    /// The pin of the certificate it presented while answering.
    gwcert: String,
    /// The session cookie it set, when it set one.
    cookie: Option<String>,
}

/// Posts one exchange and reads the gateway's answer.
async fn post(
    client: &reqwest::Client,
    gateway: &str,
    body: &str,
    dh_pubkey: Option<&str>,
) -> Result<Exchange, Error> {
    let mut request = client
        .post(format!("https://{gateway}/"))
        // The content type openconnect sends for this XML, which some
        // gateways check even though the body is plainly not a form.
        .header(
            reqwest::header::CONTENT_TYPE,
            "application/x-www-form-urlencoded",
        )
        .timeout(LOGIN_TIMEOUT)
        .body(String::from(body));

    // The key the gateway encrypts the SSO token to. Sent on every request of
    // the conversation, as openconnect does — the gateway may answer any of
    // them with the browser flow.
    if let Some(pubkey) = dh_pubkey {
        request = request
            .header("X-AnyConnect-STRAP-Pubkey", pubkey)
            .header("X-AnyConnect-STRAP-DH-Pubkey", pubkey);
    }

    let response = request.send().await.map_err(|error| {
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

    if !status.is_success() {
        return Err(unsupported(&format!(
            "the gateway answered {status} to an AnyConnect authentication"
        )));
    }
    Ok(Exchange {
        step: parse(&body)?,
        gwcert,
        cookie,
    })
}

/// The opening request: "who are you, and what do you want from me".
fn init_request(gateway: &str, sso: bool) -> String {
    // Advertised only when the profile asked for it. openconnect ships
    // `--no-external-auth` because a gateway that sees this capability can
    // *insist* on a browser where it would otherwise have served a form, so
    // offering it unasked could break a VPN that works today.
    let capabilities = if sso {
        "<capabilities>\
         <auth-method>single-sign-on-external-browser</auth-method>\
         </capabilities>"
    } else {
        ""
    };
    format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n\
         <config-auth client=\"vpn\" type=\"init\" aggregate-auth-version=\"2\">\
         <version who=\"vpn\">{VERSION}</version>\
         <device-id>{DEVICE_ID}</device-id>\
         <group-access>https://{gateway}</group-access>\
         {capabilities}\
         </config-auth>"
    )
}

/// The answer to a form.
///
/// Each input goes back as an element named after it, inside `<auth>`; the
/// group selection goes outside it, and the `<opaque>` blob is returned
/// untouched — the gateway uses it to recognise the conversation.
fn reply_request(form: &Form, answers: &[(String, String)]) -> String {
    let fields: String = answers
        .iter()
        .map(|(name, value)| format!("<{name}>{}</{name}>", escape(value)))
        .collect();
    let opaque = form.opaque.clone().unwrap_or_default();
    let group = form
        .group
        .as_ref()
        .map(|group| format!("<group-select>{}</group-select>", escape(group)))
        .unwrap_or_default();

    format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n\
         <config-auth client=\"vpn\" type=\"auth-reply\" aggregate-auth-version=\"2\">\
         <version who=\"vpn\">{VERSION}</version>\
         <device-id>{DEVICE_ID}</device-id>\
         <session-token></session-token>\
         <session-id></session-id>\
         {opaque}\
         <auth>{fields}</auth>\
         {group}\
         </config-auth>"
    )
}

/// Reads a gateway's answer to an exchange.
fn parse(body: &str) -> Result<Step, Error> {
    let Some(auth) = xml::raw_element(body, "auth") else {
        return Err(unsupported(
            "the gateway's reply is not an AnyConnect authentication",
        ));
    };

    if xml::attribute(auth, "id").as_deref() == Some("success") {
        return Ok(Step::Authenticated);
    }
    // An `<error>` is the gateway saying no, in its own words. A `<message>`
    // alongside a form is the gateway saying what to type next, which is not
    // the same thing and must not read as a failure.
    if let Some(error) = xml::value(auth, "error").filter(|text| !text.is_empty()) {
        return Err(auth_error(&error));
    }

    let Some(form) = xml::raw_element(auth, "form") else {
        return Err(unsupported(
            "the gateway asked for something that is not a form",
        ));
    };

    let fields: Vec<Field> = xml::raw_elements(form, "input")
        .into_iter()
        .filter_map(field)
        .collect();
    if fields.is_empty() {
        return Err(unsupported("the gateway's form has no fields to fill in"));
    }

    Ok(Step::Form(Box::new(Form {
        message: xml::value(auth, "message").filter(|text| !text.is_empty()),
        fields,
        opaque: xml::raw_element(body, "opaque").map(String::from),
        group: group(form),
        sso_login: xml::value(body, "sso-v2-login").filter(|url| !url.is_empty()),
    })))
}

/// One `<input>`, or `None` for the ones that are not a question — a hidden
/// field carries its own value and a button is not typed into.
fn field(input: &str) -> Option<Field> {
    let kind = xml::attribute(input, "type").unwrap_or_default();
    if matches!(kind.as_str(), "hidden" | "submit" | "button") {
        return None;
    }
    let name = xml::attribute(input, "name").filter(|name| !name.is_empty())?;
    let label = xml::attribute(input, "label")
        .filter(|label| !label.is_empty())
        .unwrap_or_else(|| name.clone());
    Some(Field {
        secret: kind == "password",
        sso: kind == "sso",
        // Gateways label their fields "Username:", which reads badly next to
        // an entry box that already looks like one.
        label: String::from(label.trim_end_matches(':').trim()),
        name,
    })
}

/// The tunnel group to select: whichever option the gateway marked selected,
/// else the first it offered.
fn group(form: &str) -> Option<String> {
    let select = xml::raw_element(form, "select")?;
    let options = xml::raw_elements(select, "option");
    let selected = options.iter().find(|option| {
        xml::attribute(option, "selected")
            .is_some_and(|value| value == "true" || value.eq_ignore_ascii_case("yes"))
    });
    let chosen = selected.or_else(|| options.first())?;
    xml::attribute(chosen, "value")
        .or_else(|| xml::value(chosen, "option"))
        .filter(|group| !group.is_empty())
}

/// Asks the user for the fields the gateway wants, filling in what is already
/// known — the stored username, and the password from a previous sign-in.
async fn answer(
    profile: &Profile,
    form: &Form,
    keys: &mut Option<sso::Keys>,
    state: &SecretAgentState,
) -> Result<Vec<(String, String)>, Error> {
    let stored_password = super::cache::password(&profile.uuid);
    let mut known: Vec<(String, String)> = Vec::new();
    let mut ask: Vec<SecretField> = Vec::new();

    // An `<input type="sso">` is answered by the browser, not by the user, so
    // it is resolved before anything is prompted for — otherwise the prompt
    // would sit there asking for a token nobody can type.
    let sso_token = match form.fields.iter().find(|field| field.sso) {
        Some(_) => Some(browser_token(form, keys).await?),
        None => None,
    };

    for field in &form.fields {
        if field.sso {
            known.push((field.name.clone(), sso_token.clone().unwrap_or_default()));
            continue;
        }
        let known_value = match field.name.as_str() {
            "username" => profile.username.clone(),
            "password" => stored_password.clone(),
            _ => None,
        };
        match known_value {
            Some(value) => known.push((field.name.clone(), value)),
            None => ask.push(SecretField {
                key: field.name.clone(),
                label: field.label.clone(),
                secret: field.secret,
            }),
        }
    }

    if ask.is_empty() {
        return Ok(known);
    }

    let values = state
        .prompt(SecretRequest {
            uuid: profile.uuid.clone(),
            name: profile.name.clone(),
            setting: String::from("vpn"),
            message: form.message.clone(),
            fields: ask,
        })
        .await
        .ok_or_else(|| auth_error("sign-in dismissed"))?;

    let mut answers = known;
    for field in &form.fields {
        if answers.iter().any(|(name, _)| *name == field.name) {
            continue;
        }
        answers.push((
            field.name.clone(),
            values.get(&field.name).cloned().unwrap_or_default(),
        ));
    }
    // Back into the gateway's order: it is the order the form was written in,
    // and some gateways read the elements positionally.
    answers.sort_by_key(|(name, _)| {
        form.fields
            .iter()
            .position(|field| field.name == *name)
            .unwrap_or(usize::MAX)
    });
    Ok(answers)
}

/// Runs the browser sign-in and opens the token it comes back with.
///
/// # Errors
///
/// Returns an error when the gateway asked for a browser sign-in without
/// saying where to go, when the profile did not enable the flow (so there is
/// no key to decrypt with), or when the sign-in does not complete.
async fn browser_token(form: &Form, keys: &mut Option<sso::Keys>) -> Result<String, Error> {
    let Some(url) = form.sso_login.as_deref() else {
        return Err(unsupported(
            "the gateway wants a browser sign-in but did not say where to go",
        ));
    };
    // Taken, not borrowed: the shared secret is ephemeral and protects
    // exactly one token, so the key cannot outlive this use. A gateway that
    // asks twice therefore gets an error rather than a reused secret.
    let keys = keys.take().ok_or_else(|| {
        unsupported("this gateway requires a browser sign-in; enable it on the VPN profile")
    })?;

    let blob = sso::await_token(url, SSO_TIMEOUT).await?;
    let bytes = sso::base64_decode(&blob)
        .ok_or_else(|| auth_error("the browser returned a token that is not base64"))?;
    let parsed = sso::parse_blob(&bytes)?;
    sso::decrypt(keys, &parsed)
}

/// The password out of a set of answers, when the form asked for one.
fn password_of(form: &Form, answers: &[(String, String)]) -> Option<String> {
    let field = form.fields.iter().find(|field| field.name == "password")?;
    answers
        .iter()
        .find(|(name, _)| *name == field.name)
        .map(|(_, value)| value.clone())
        .filter(|value| !value.is_empty())
}

/// The `webvpn=…` cookie the gateway set, as openconnect's `--cookie` wants
/// it: the name and the value, without the attributes that follow.
///
/// Read off the headers rather than through a cookie jar, because the name is
/// the only thing that identifies it and an empty value is a deletion — which
/// is what a gateway sends on the *first* exchange, before there is a session.
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

/// XML-escapes a value going back to the gateway. A password containing `&`
/// or `<` would otherwise produce a document the gateway cannot parse — and
/// the failure would look like a wrong password.
fn escape(value: &str) -> String {
    let mut out = String::with_capacity(value.len());
    for character in value.chars() {
        match character {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&apos;"),
            _ => out.push(character),
        }
    }
    out
}

fn auth_error(message: &str) -> Error {
    Error::VpnAuthenticationFailed(String::from(message))
}

fn unsupported(message: &str) -> Error {
    Error::VpnProtocolUnsupported(String::from(message))
}

#[cfg(test)]
// One test sets `SSL_CERT_FILE` so the mock gateway's committed certificate
// verifies through the same path a real one does.
#[allow(unsafe_code)]
mod tests {
    use super::*;

    /// A gateway's opening form, as an ASA writes it.
    const MAIN_FORM: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\
        <config-auth client=\"vpn\" type=\"auth-request\" aggregate-auth-version=\"2\">\
        <opaque is-for=\"sg\"><tunnel-group>DefaultWEBVPNGroup</tunnel-group>\
        <config-hash>1699999999999</config-hash></opaque>\
        <auth id=\"main\"><title>Login</title>\
        <message>Please enter your username and password.</message>\
        <form><input type=\"text\" name=\"username\" label=\"Username:\"/>\
        <input type=\"password\" name=\"password\" label=\"Password:\"/>\
        <input type=\"hidden\" name=\"tgroup\"/>\
        <select name=\"group_list\" label=\"GROUP:\">\
        <option value=\"Employees\">Employees</option>\
        <option value=\"Contractors\" selected=\"true\">Contractors</option>\
        </select></form></auth></config-auth>";

    const SUCCESS: &str = "<config-auth client=\"vpn\" type=\"complete\">\
        <auth id=\"success\"><title>SSL VPN Service</title></auth>\
        <session-token>TOKEN</session-token></config-auth>";

    fn form_of(body: &str) -> Form {
        match parse(body).expect("a form") {
            Step::Form(form) => *form,
            Step::Authenticated => panic!("expected a form"),
        }
    }

    #[test]
    fn a_gateways_form_becomes_the_questions_to_ask() {
        let form = form_of(MAIN_FORM);
        assert_eq!(
            form.message.as_deref(),
            Some("Please enter your username and password.")
        );
        // Hidden fields are not questions, and the label loses the colon the
        // gateway wrote for its own web form.
        assert_eq!(
            form.fields,
            vec![
                Field {
                    name: String::from("username"),
                    label: String::from("Username"),
                    secret: false,
                    sso: false,
                },
                Field {
                    name: String::from("password"),
                    label: String::from("Password"),
                    secret: true,
                    sso: false,
                },
            ]
        );
        // The gateway recognises its own conversation by this blob, so it goes
        // back exactly as it came.
        assert_eq!(
            form.opaque.as_deref(),
            Some(
                "<opaque is-for=\"sg\"><tunnel-group>DefaultWEBVPNGroup</tunnel-group>\
                 <config-hash>1699999999999</config-hash></opaque>"
            )
        );
        // The option the gateway marked selected, not merely the first.
        assert_eq!(form.group.as_deref(), Some("Contractors"));
    }

    #[test]
    fn a_success_is_recognised_and_a_form_is_not() {
        assert_eq!(parse(SUCCESS).expect("success"), Step::Authenticated);
        assert!(matches!(parse(MAIN_FORM), Ok(Step::Form(_))));
    }

    #[test]
    fn a_rejection_surfaces_the_gateways_own_message() {
        let refused = "<config-auth type=\"auth-request\"><auth id=\"main\">\
            <error id=\"88\" param1=\"\">Login failed.</error>\
            <form><input type=\"text\" name=\"username\"/></form></auth></config-auth>";
        let error = parse(refused).expect_err("an error is a refusal");
        assert!(error.to_string().contains("Login failed."), "got: {error}");
        // A refusal is the user's problem to fix, not another agent's.
        assert!(matches!(error, Error::VpnAuthenticationFailed(_)));
    }

    #[test]
    fn a_reply_that_is_not_anyconnect_is_left_to_someone_else() {
        // The whole point of the distinction: these must reach NM as
        // NoSecrets, so the plugin's own auth dialog still gets its turn.
        for body in [
            "<html><body>Not a gateway</body></html>",
            "",
            "<config-auth type=\"auth-request\"><auth id=\"main\"><message>hi</message></auth>\
             </config-auth>",
            "<config-auth><auth id=\"main\"><form></form></auth></config-auth>",
        ] {
            let error = parse(body).expect_err("not understood");
            assert!(
                matches!(error, Error::VpnProtocolUnsupported(_)),
                "{body:?} produced {error}"
            );
        }
    }

    #[test]
    fn a_challenge_form_asks_only_for_the_second_factor() {
        let challenge = "<config-auth type=\"auth-request\"><auth id=\"challenge\">\
            <message>Answer with the code from your token.</message>\
            <form><input type=\"password\" name=\"secondary_password\" label=\"Code:\"/></form>\
            </auth></config-auth>";
        let form = form_of(challenge);
        assert_eq!(form.fields.len(), 1);
        assert!(form.fields[0].secret);
        assert_eq!(form.fields[0].name, "secondary_password");
        assert_eq!(
            form.message.as_deref(),
            Some("Answer with the code from your token.")
        );
    }

    #[test]
    fn the_reply_carries_every_answer_the_opaque_blob_and_the_group() {
        let form = form_of(MAIN_FORM);
        let answers = vec![
            (String::from("username"), String::from("alice")),
            (String::from("password"), String::from("hunter2")),
        ];
        let reply = reply_request(&form, &answers);

        assert!(reply.contains("type=\"auth-reply\""), "got: {reply}");
        assert!(reply.contains("<username>alice</username>"), "got: {reply}");
        assert!(
            reply.contains("<password>hunter2</password>"),
            "got: {reply}"
        );
        assert!(
            reply.contains("<opaque is-for=\"sg\"><tunnel-group>DefaultWEBVPNGroup"),
            "the gateway drops a reply that does not echo its opaque blob: {reply}"
        );
        assert!(
            reply.contains("<group-select>Contractors</group-select>"),
            "got: {reply}"
        );
    }

    #[test]
    fn a_password_with_markup_in_it_cannot_break_the_document() {
        let form = Form {
            fields: vec![Field {
                name: String::from("password"),
                label: String::from("Password"),
                secret: true,
                sso: false,
            }],
            ..Form::default()
        };
        let answers = vec![(String::from("password"), String::from("a<b&c\"d"))];
        let reply = reply_request(&form, &answers);
        assert!(
            reply.contains("<password>a&lt;b&amp;c&quot;d</password>"),
            "got: {reply}"
        );
        // And it reads back as what was typed, not as the escaping.
        assert_eq!(xml::value(&reply, "password").as_deref(), Some("a<b&c\"d"));
    }

    #[test]
    fn the_session_cookie_is_the_one_openconnect_takes() {
        let mut headers = reqwest::header::HeaderMap::new();
        headers.append(
            reqwest::header::SET_COOKIE,
            "webvpn=SESSIONVALUE; path=/; secure; HttpOnly"
                .parse()
                .expect("header"),
        );
        assert_eq!(
            session_cookie(&headers).as_deref(),
            Some("webvpn=SESSIONVALUE")
        );
    }

    #[test]
    fn a_cleared_or_absent_cookie_is_not_a_session() {
        // A gateway clears `webvpn` on the first exchange, before there is a
        // session; taking that as the cookie would hand the plugin an empty
        // one and fail at connect time instead of at sign-in time.
        let mut headers = reqwest::header::HeaderMap::new();
        headers.append(
            reqwest::header::SET_COOKIE,
            "webvpn=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/"
                .parse()
                .expect("header"),
        );
        headers.append(
            reqwest::header::SET_COOKIE,
            "webvpnc=bu:/CACHE/; path=/".parse().expect("header"),
        );
        assert_eq!(session_cookie(&headers), None);
        assert_eq!(session_cookie(&reqwest::header::HeaderMap::new()), None);
    }

    #[test]
    fn the_opening_request_names_the_gateway_it_is_asking() {
        let request = init_request("vpn.example.com", false);
        assert!(request.contains("type=\"init\""), "got: {request}");
        assert!(
            request.contains("<group-access>https://vpn.example.com</group-access>"),
            "got: {request}"
        );
        // No SSO capability is advertised: claiming one wayle cannot complete
        // would make a gateway answer with a SAML redirect instead of a form.
        assert!(!request.contains("single-sign-on"), "got: {request}");
    }

    #[test]
    fn only_the_first_password_is_remembered() {
        let form = form_of(MAIN_FORM);
        let answers = vec![
            (String::from("username"), String::from("alice")),
            (String::from("password"), String::from("hunter2")),
        ];
        assert_eq!(password_of(&form, &answers).as_deref(), Some("hunter2"));

        // A second factor's answer is different every time, so caching it
        // would guarantee the next connect fails.
        let challenge = Form {
            fields: vec![Field {
                name: String::from("secondary_password"),
                label: String::from("Code"),
                secret: true,
                sso: false,
            }],
            ..Form::default()
        };
        let code = vec![(String::from("secondary_password"), String::from("123456"))];
        assert_eq!(password_of(&challenge, &code), None);
    }
}

/// Tests against the mock AnyConnect gateway in `tests/mock-gateway`, started
/// by `just test-gateway`.
///
/// The whole conversation over a real TLS connection: the opening request, the
/// form, the challenge, the echoed `<opaque>` blob the gateway refuses a reply
/// without, the session cookie and the certificate pin. Nothing here touches a
/// real VPN.
#[cfg(test)]
#[allow(unsafe_code)]
mod mock {
    use std::{collections::HashMap, sync::Arc};

    use futures::StreamExt;

    use super::*;

    const GATEWAY: &str = "127.0.0.1:8445";
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
            uuid: String::from("mock-anyconnect"),
            name: String::from("Mock"),
            gateway: String::from(GATEWAY),
            protocol: String::from("anyconnect"),
            username: None,
            sso: false,
        }
    }

    /// Answers whatever the gateway asks for, the way the user would.
    ///
    /// Driven off the prompt property rather than pre-seeded, so this covers
    /// the path the shell takes: the gateway's field names reach the form and
    /// the answers come back keyed by them.
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
                ("secondary_password", "123456"),
            ]),
        );

        let signed_in = sign_in(&profile(), &client(), &state)
            .await
            .expect("the mock gateway signs us in");

        // The cookie openconnect takes, in the shape it takes it.
        assert_eq!(signed_in.session.cookie, "webvpn=SESSIONVALUE");
        assert_eq!(signed_in.session.host, GATEWAY);
        assert_eq!(signed_in.session.gwcert, PIN);
        // The password is worth caching; the second factor never is.
        assert_eq!(signed_in.remember_password.as_deref(), Some("hunter2"));
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
        assert!(error.to_string().contains("Login failed."), "got: {error}");
        // A refusal must not read as "wayle does not speak this protocol", or
        // NM would quietly hand a wrong password to the next agent instead of
        // telling the user.
        assert!(matches!(error, Error::VpnAuthenticationFailed(_)));
    }

    #[tokio::test]
    #[ignore = "needs the mock gateway: just test-gateway"]
    async fn a_globalprotect_gateway_is_not_mistaken_for_an_anyconnect_one() {
        // The GlobalProtect mock on 8443 answers this exchange with something
        // that is not a config-auth, which has to reach NM as "someone else
        // should try" rather than as a failed sign-in.
        let state = Arc::new(SecretAgentState::new());
        let mut profile = profile();
        profile.gateway = String::from("127.0.0.1:8443");

        let error = sign_in(&profile, &client(), &state)
            .await
            .expect_err("that gateway does not speak AnyConnect");
        assert!(
            matches!(error, Error::VpnProtocolUnsupported(_)),
            "got: {error}"
        );
    }
}

/// The browser sign-in's own tests: the capability is off unless asked for,
/// and an `sso` input is recognised as one the user cannot answer.
#[cfg(test)]
mod sso_tests {
    use super::*;

    #[test]
    fn the_browser_capability_is_not_advertised_unless_the_profile_asks() {
        // The reason this matters: openconnect's manual warns that a gateway
        // seeing this capability may *insist* on a browser where it would
        // otherwise have served a form. Advertising it unasked could break a
        // VPN that works today.
        let quiet = init_request("vpn.example.com", false);
        assert!(
            !quiet.contains("single-sign-on"),
            "the capability leaked into an ordinary sign-in: {quiet}"
        );
        assert!(!quiet.contains("<capabilities>"), "got: {quiet}");

        let asked = init_request("vpn.example.com", true);
        assert!(
            asked.contains("<auth-method>single-sign-on-external-browser</auth-method>"),
            "got: {asked}"
        );
    }

    #[test]
    fn an_sso_input_is_a_field_the_user_cannot_answer() {
        let form = parse(
            "<config-auth><opaque is-for=\"sg\"><x>1</x></opaque>\
             <auth id=\"main\"><form>\
             <input type=\"sso\" name=\"sso-token\" label=\"Single sign-on\"/>\
             </form></auth>\
             <sso-v2-login>https://idp.example.com/saml</sso-v2-login>\
             </config-auth>",
        )
        .expect("a form");
        let Step::Form(form) = form else {
            panic!("expected a form");
        };

        assert_eq!(form.fields.len(), 1);
        assert!(form.fields[0].sso, "an sso input must be flagged as one");
        assert_eq!(
            form.sso_login.as_deref(),
            Some("https://idp.example.com/saml"),
            "the URL to open has to come out of the same reply"
        );
    }

    #[test]
    fn an_ordinary_form_carries_no_sso_login_and_no_sso_field() {
        let form = parse(
            "<config-auth><auth id=\"main\"><form>\
             <input type=\"text\" name=\"username\" label=\"Username:\"/>\
             </form></auth></config-auth>",
        )
        .expect("a form");
        let Step::Form(form) = form else {
            panic!("expected a form");
        };
        assert!(!form.fields[0].sso);
        assert!(form.sso_login.is_none());
    }

    #[tokio::test]
    async fn a_gateway_asking_for_a_browser_when_the_profile_did_not_is_unsupported() {
        // Reaches NM as "someone else should try" rather than as a failed
        // sign-in: the plugin's own auth dialog can do a webview, and this
        // cannot.
        let form = Form {
            fields: vec![Field {
                name: String::from("sso-token"),
                label: String::from("Single sign-on"),
                secret: false,
                sso: true,
            }],
            sso_login: Some(String::from("https://idp.example.com/saml")),
            ..Form::default()
        };
        let error = browser_token(&form, &mut None)
            .await
            .expect_err("with no key there is nothing to decrypt with");
        assert!(
            matches!(error, Error::VpnProtocolUnsupported(_)),
            "got {error:?}"
        );
    }

    #[tokio::test]
    async fn a_browser_sign_in_with_nowhere_to_go_is_refused_before_a_browser_opens() {
        let form = Form {
            fields: vec![Field {
                name: String::from("sso-token"),
                label: String::from("Single sign-on"),
                secret: false,
                sso: true,
            }],
            // The gateway asked for SSO but sent no `sso-v2-login`.
            sso_login: None,
            ..Form::default()
        };
        let mut keys = Some(sso::Keys::generate().expect("keys"));
        let error = browser_token(&form, &mut keys)
            .await
            .expect_err("nowhere to send the user");
        assert!(
            matches!(error, Error::VpnProtocolUnsupported(_)),
            "got {error:?}"
        );
        // And the key is still there: nothing was consumed by a flow that
        // never started.
        assert!(keys.is_some());
    }
}
