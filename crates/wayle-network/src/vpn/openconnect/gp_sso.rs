//! GlobalProtect SAML sign-in through the system browser.
//!
//! A SAML gateway answers prelogin with `saml-auth-method` and `saml-request`
//! instead of field labels. Completing that needs whatever the identity
//! provider asks for — a password, a push, a hardware key, a corporate SSO
//! page — which is a browser's job, not a form's.
//!
//! The expensive way to do this is to embed one:
//! `GlobalProtect-openconnect` links webkit so it can watch the IdP navigation
//! and lift the cookie out of a response header. wayle does not, and will not:
//! webkitgtk is a very large dependency for one sign-in flow.
//!
//! The cheap way is the one recent GlobalProtect clients use. The portal hands
//! the sign-in to the real browser and takes the answer back through a custom
//! URI scheme, `globalprotectcallback:`. wayle registers a handler for that
//! scheme, the browser hands the payload to `wayle vpn sso-callback`, and it
//! reaches the waiting sign-in over the shell's D-Bus interface.
//!
//! # What is and is not verified
//!
//! Pinned by the tests below, from openconnect's `auth-globalprotect.c` and
//! the shapes GlobalProtect actually sends: the prelogin fields, `REDIRECT`
//! versus `POST`, the base64 of `saml-request`, and the refusal of a
//! `saml-request` that decodes to something that is not a URL.
//!
//! **Not** verified, because it needs a real SAML portal and a person to sign
//! in: the callback payload's exact encoding. Palo Alto documents neither the
//! scheme nor the payload, and the field names below are what the same values
//! are called in the HTTP headers of the non-browser flow. [`parse_callback`]
//! therefore reads all three shapes those values are known to travel in —
//! header lines, XML elements, and HTML `<meta>` tags — rather than betting on
//! one. A payload in none of them is an error rather than a guess.
//!
//! Nothing here runs unless the profile asks for it (`wayle-sso`), so a
//! gateway that works today keeps working exactly as it did.

use base64::Engine;

use super::form;
use crate::Error;

/// How the portal wants the SAML request delivered.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum Method {
    /// `saml-request` is a URL to open.
    Redirect,
    /// `saml-request` is an HTML document to render, which posts itself to the
    /// identity provider.
    Post,
}

/// The sign-in a SAML gateway wants performed in a browser.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct SamlRequest {
    /// How to deliver it.
    pub method: Method,
    /// The decoded request: a URL for [`Method::Redirect`], an HTML document
    /// for [`Method::Post`].
    pub payload: String,
}

/// What the browser hands back once the identity provider is satisfied.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub(super) struct Callback {
    /// Who signed in, as the portal understands it. Becomes the username for
    /// the login that follows.
    pub username: Option<String>,
    /// The gateway's pre-login cookie.
    pub prelogin_cookie: Option<String>,
    /// The portal's user-auth cookie, sent instead of the above by a portal
    /// rather than a gateway.
    pub portal_cookie: Option<String>,
}

impl Callback {
    /// The cookie to sign in with, whichever of the two the portal sent.
    pub fn cookie(&self) -> Option<&str> {
        self.prelogin_cookie
            .as_deref()
            .or(self.portal_cookie.as_deref())
            .filter(|cookie| !cookie.is_empty())
    }

    /// Whether this carries enough to continue.
    pub fn is_complete(&self) -> bool {
        self.cookie().is_some()
    }
}

/// The URI scheme GlobalProtect sends its browser answer back on.
pub(super) const CALLBACK_SCHEME: &str = "globalprotectcallback";

/// Reads the SAML request out of a prelogin response.
///
/// Returns `None` for an ordinary gateway, which is every gateway that sends
/// field labels instead.
pub(super) fn parse_request(body: &str) -> Option<SamlRequest> {
    let method = super::xml::value(body, "saml-auth-method")?;
    let request = super::xml::value(body, "saml-request")?;
    if request.is_empty() {
        return None;
    }

    let method = match method.trim().to_ascii_uppercase().as_str() {
        "REDIRECT" => Method::Redirect,
        "POST" => Method::Post,
        // An unknown method is not something to guess at: the payload's
        // meaning is exactly what the method names.
        _ => return None,
    };

    // Whitespace is not part of base64 but gateways wrap the value anyway.
    let compact: String = request.chars().filter(|c| !c.is_whitespace()).collect();
    let decoded = base64::engine::general_purpose::STANDARD
        .decode(&compact)
        .ok()?;
    let payload = String::from_utf8(decoded).ok()?;

    Some(SamlRequest { method, payload })
}

/// Whether a decoded `REDIRECT` payload is a URL worth opening.
///
/// The payload goes to the system browser, so anything but `https:` (or
/// `http:`, which some internal portals still use) is refused rather than
/// handed to `xdg-open` — a `file:` or a `javascript:` here would be the
/// gateway choosing what runs on this machine.
pub(super) fn is_openable_url(payload: &str) -> bool {
    let lower = payload.trim().to_ascii_lowercase();
    (lower.starts_with("https://") || lower.starts_with("http://"))
        && !payload.trim().contains(char::is_whitespace)
}

/// Reads the payload the browser handed back.
///
/// Accepts the callback URI whole (`globalprotectcallback:<payload>`) or just
/// the payload. The payload is base64 in the flows seen so far, but a portal
/// that sends it in the clear is read the same way rather than refused.
///
/// # Errors
///
/// Returns an error when the URI is for another scheme, or when the payload
/// carries none of the fields the sign-in needs.
pub(super) fn parse_callback(uri: &str) -> Result<Callback, Error> {
    let trimmed = uri.trim();
    let payload = match trimmed.split_once(':') {
        Some((scheme, rest)) if scheme.eq_ignore_ascii_case(CALLBACK_SCHEME) => rest,
        // Not a scheme we answer for. A bare payload has no colon at all, or
        // its first colon belongs to a header line inside it.
        Some((scheme, _)) if scheme.contains(char::is_whitespace) || scheme.is_empty() => trimmed,
        Some(_) if !trimmed.contains('\n') && trimmed.contains("://") => {
            return Err(auth_error(
                "the browser answered with an unexpected address",
            ));
        }
        _ => trimmed,
    };

    let payload = payload.trim_start_matches('/');
    let text = decode_payload(payload);
    let callback = read_fields(&text);

    if callback.is_complete() {
        return Ok(callback);
    }
    Err(auth_error(
        "the browser sign-in did not return a gateway cookie",
    ))
}

/// Base64-decodes when it can, and hands back the text unchanged when it
/// cannot: both shapes have been seen, and a portal sending plain XML should
/// not look like a failure.
fn decode_payload(payload: &str) -> String {
    let compact: String = payload.chars().filter(|c| !c.is_whitespace()).collect();
    let decoders = [
        base64::engine::general_purpose::STANDARD,
        base64::engine::general_purpose::URL_SAFE,
    ];

    for decoder in decoders {
        if let Ok(bytes) = decoder.decode(&compact)
            && let Ok(text) = String::from_utf8(bytes)
            && text.chars().any(|c| c.is_ascii_alphabetic())
        {
            return text;
        }
    }
    // Percent-encoding is what a browser does to a URI's tail.
    form::decode_component(payload)
}

/// The three shapes the same values travel in.
///
/// The non-browser flow sends them as HTTP headers, the portal's own success
/// page carries them as `<meta>` tags, and some responses are plain XML. All
/// three name the fields identically, which is what makes reading all three
/// cheaper than picking one and being wrong.
fn read_fields(text: &str) -> Callback {
    Callback {
        username: field(text, "saml-username"),
        prelogin_cookie: field(text, "prelogin-cookie"),
        portal_cookie: field(text, "portal-userauthcookie"),
    }
}

/// One field, in whichever shape it appears.
fn field(text: &str, name: &str) -> Option<String> {
    xml_element(text, name)
        .or_else(|| meta_tag(text, name))
        .or_else(|| header_line(text, name))
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
}

/// `<name>value</name>`.
fn xml_element(text: &str, name: &str) -> Option<String> {
    super::xml::value(text, name)
}

/// `<meta name="name" content="value">`, in either attribute order.
fn meta_tag(text: &str, name: &str) -> Option<String> {
    let lower = text.to_ascii_lowercase();
    let needle = format!("name=\"{name}\"");
    let at = lower.find(&needle)?;

    // The content attribute may sit on either side of the name attribute
    // within the same tag, so search the tag rather than the remainder.
    let start = lower[..at].rfind('<')?;
    let end = lower[at..].find('>').map(|offset| at + offset)?;
    let tag = &text[start..end];

    let lower_tag = tag.to_ascii_lowercase();
    let content = lower_tag.find("content=\"")? + "content=\"".len();
    let rest = &tag[content..];
    let close = rest.find('"')?;
    Some(rest[..close].to_owned())
}

/// `name: value` on its own line.
fn header_line(text: &str, name: &str) -> Option<String> {
    text.lines().find_map(|line| {
        let (key, value) = line.split_once(':')?;
        key.trim()
            .eq_ignore_ascii_case(name)
            .then(|| value.to_owned())
    })
}

fn auth_error(message: &str) -> Error {
    Error::VpnAuthenticationFailed(String::from(message))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn encode(text: &str) -> String {
        base64::engine::general_purpose::STANDARD.encode(text)
    }

    fn prelogin(method: &str, request: &str) -> String {
        format!(
            "<prelogin-response><status>Success</status>\
             <saml-auth-method>{method}</saml-auth-method>\
             <saml-request>{request}</saml-request></prelogin-response>"
        )
    }

    #[test]
    fn a_redirect_portal_yields_the_url_to_open() {
        let body = prelogin("REDIRECT", &encode("https://idp.example.com/saml?x=1"));

        let request = parse_request(&body).expect("a SAML request");

        assert_eq!(request.method, Method::Redirect);
        assert_eq!(request.payload, "https://idp.example.com/saml?x=1");
        assert!(is_openable_url(&request.payload));
    }

    #[test]
    fn a_post_portal_yields_the_html_that_submits_itself() {
        let html = "<html><body onload=\"document.f.submit()\"><form name=f></form></body></html>";
        let body = prelogin("POST", &encode(html));

        let request = parse_request(&body).expect("a SAML request");

        assert_eq!(request.method, Method::Post);
        assert_eq!(request.payload, html);
        // It is a document, not an address: opening it as a URL would be wrong.
        assert!(!is_openable_url(&request.payload));
    }

    #[test]
    fn a_wrapped_base64_value_still_decodes() {
        // Gateways wrap long values; whitespace is not part of base64.
        let wrapped = format!(
            "{}\n  {}",
            &encode("https://idp.example.com/a")[..8],
            &encode("https://idp.example.com/a")[8..]
        );
        let body = prelogin("redirect", &wrapped);

        let request = parse_request(&body).expect("a SAML request");

        assert_eq!(request.payload, "https://idp.example.com/a");
        // The method is matched case-insensitively, as sent.
        assert_eq!(request.method, Method::Redirect);
    }

    #[test]
    fn an_ordinary_gateway_has_no_saml_request() {
        let body = "<prelogin-response><status>Success</status>\
                    <username-label>Username</username-label></prelogin-response>";

        assert_eq!(parse_request(body), None);
    }

    #[test]
    fn a_request_this_code_cannot_carry_out_is_not_guessed_at() {
        // An unknown method: the payload's meaning is what the method names.
        assert_eq!(parse_request(&prelogin("CAS", &encode("https://a"))), None);
        // Present but empty.
        assert_eq!(parse_request(&prelogin("REDIRECT", "")), None);
        // Not base64 at all.
        assert_eq!(
            parse_request(&prelogin("REDIRECT", "!!!not base64!!!")),
            None
        );
    }

    #[test]
    fn only_http_addresses_are_handed_to_the_browser() {
        assert!(is_openable_url("https://idp.example.com/saml"));
        assert!(is_openable_url("http://portal.internal/saml"));
        // The gateway does not get to choose what runs on this machine.
        assert!(!is_openable_url("file:///etc/passwd"));
        assert!(!is_openable_url("javascript:alert(1)"));
        assert!(!is_openable_url("xdg-open;rm -rf /"));
        // A URL with a space in it is a command line, not an address.
        assert!(!is_openable_url("https://a.example.com /etc/passwd"));
        assert!(!is_openable_url(""));
    }

    #[test]
    fn the_callback_reads_xml_fields() {
        let payload = encode(
            "<saml-auth-status>1</saml-auth-status>\
             <saml-username>alice@example.com</saml-username>\
             <prelogin-cookie>COOKIE-VALUE</prelogin-cookie>",
        );

        let callback = parse_callback(&format!("globalprotectcallback:{payload}"))
            .expect("a complete callback");

        assert_eq!(callback.username.as_deref(), Some("alice@example.com"));
        assert_eq!(callback.cookie(), Some("COOKIE-VALUE"));
    }

    #[test]
    fn the_callback_reads_meta_tags() {
        let payload = encode(
            "<html><head>\
             <meta name=\"saml-username\" content=\"bob@example.com\">\
             <meta content=\"META-COOKIE\" name=\"prelogin-cookie\">\
             </head></html>",
        );

        let callback = parse_callback(&payload).expect("a complete callback");

        assert_eq!(callback.username.as_deref(), Some("bob@example.com"));
        // Attribute order is not fixed, so both orders have to read.
        assert_eq!(callback.cookie(), Some("META-COOKIE"));
    }

    #[test]
    fn the_callback_reads_header_lines() {
        let payload = encode(
            "saml-auth-status: 1\r\n\
             saml-username: carol@example.com\r\n\
             portal-userauthcookie: PORTAL-COOKIE\r\n",
        );

        let callback = parse_callback(&payload).expect("a complete callback");

        assert_eq!(callback.username.as_deref(), Some("carol@example.com"));
        // A portal sends its own cookie where a gateway sends prelogin-cookie.
        assert_eq!(callback.cookie(), Some("PORTAL-COOKIE"));
    }

    #[test]
    fn the_gateway_cookie_wins_when_both_are_sent() {
        let callback = Callback {
            username: None,
            prelogin_cookie: Some(String::from("GATEWAY")),
            portal_cookie: Some(String::from("PORTAL")),
        };

        assert_eq!(callback.cookie(), Some("GATEWAY"));
    }

    #[test]
    fn a_plain_unencoded_payload_is_read_too() {
        let callback =
            parse_callback("globalprotectcallback:<prelogin-cookie>PLAIN</prelogin-cookie>")
                .expect("a complete callback");

        assert_eq!(callback.cookie(), Some("PLAIN"));
    }

    #[test]
    fn a_callback_without_a_cookie_is_an_error_not_a_half_sign_in() {
        // The IdP said who, but the portal sent nothing to sign in with.
        let payload = encode("<saml-username>dave@example.com</saml-username>");
        assert!(parse_callback(&payload).is_err());

        // Empty cookie elements are not cookies.
        assert!(parse_callback(&encode("<prelogin-cookie></prelogin-cookie>")).is_err());
        assert!(parse_callback("").is_err());
        assert!(parse_callback("globalprotectcallback:").is_err());
    }

    #[test]
    fn another_schemes_uri_is_refused() {
        let error = parse_callback("https://example.com/?prelogin-cookie=NOPE");

        assert!(error.is_err());
    }
}

/// The sign-in currently waiting for a browser callback, if any.
///
/// One at a time: the URI scheme carries no correlation id, so a second
/// concurrent sign-in would have no way to tell whose answer arrived. Starting
/// one cancels the one before it, which is what a user retrying a stuck
/// sign-in expects.
static PENDING: std::sync::Mutex<Option<tokio::sync::oneshot::Sender<String>>> =
    std::sync::Mutex::new(None);

/// Hands a `globalprotectcallback:` URI to whatever sign-in is waiting for it.
///
/// Called from the shell's D-Bus interface, which is what the desktop entry
/// for the scheme routes the browser's answer to.
///
/// Returns whether anything was waiting; a callback that arrives with no
/// sign-in in progress is stale (a re-opened tab, a second click) and is
/// dropped rather than kept for the next one.
pub(crate) fn deliver_callback(uri: &str) -> bool {
    let Ok(mut pending) = PENDING.lock() else {
        return false;
    };
    match pending.take() {
        Some(sender) => sender.send(uri.to_owned()).is_ok(),
        None => false,
    }
}

/// Opens the browser at the identity provider and waits for the answer.
///
/// # Errors
///
/// Returns an error when the request is not one this code can carry out, when
/// no browser could be opened, or when nothing came back within `timeout`.
pub(super) async fn sign_in(
    request: &SamlRequest,
    timeout: std::time::Duration,
) -> Result<Callback, Error> {
    let url = browser_url(request)?;

    let (sender, receiver) = tokio::sync::oneshot::channel();
    // Registered before the browser opens: the identity provider can answer
    // faster than this task is scheduled again, and a callback with nothing
    // waiting is dropped.
    if let Ok(mut pending) = PENDING.lock() {
        *pending = Some(sender);
    }

    if let Err(error) = super::sso::open_in_browser(&url) {
        if let Ok(mut pending) = PENDING.lock() {
            *pending = None;
        }
        return Err(error);
    }
    tracing::debug!("waiting for the GlobalProtect browser sign-in");

    let answer = tokio::time::timeout(timeout, receiver).await;
    if let Ok(mut pending) = PENDING.lock() {
        *pending = None;
    }

    match answer {
        Ok(Ok(uri)) => parse_callback(&uri),
        // The sender was dropped: another sign-in replaced this one.
        Ok(Err(_)) => Err(auth_error("the browser sign-in was replaced by another")),
        Err(_) => Err(auth_error("the browser sign-in was not completed in time")),
    }
}

/// The address to hand the browser.
///
/// A `POST` request is an HTML document rather than an address, so it is
/// written to a file the browser can open. The file is created private to this
/// user and removed by the OS's temporary directory policy rather than kept:
/// it holds a SAML request, which identifies the user to the identity
/// provider.
fn browser_url(request: &SamlRequest) -> Result<String, Error> {
    match request.method {
        Method::Redirect => {
            if !is_openable_url(&request.payload) {
                return Err(auth_error(
                    "the gateway asked to open something that is not a web address",
                ));
            }
            Ok(request.payload.clone())
        }
        Method::Post => {
            let path = write_request_document(&request.payload)?;
            Ok(format!("file://{path}"))
        }
    }
}

/// Writes the self-submitting form somewhere the browser can open it.
fn write_request_document(html: &str) -> Result<String, Error> {
    use std::io::Write as _;

    let name = format!(
        "wayle-gp-saml-{}-{}.html",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|since| since.as_nanos())
            .unwrap_or_default()
    );
    let path = std::env::temp_dir().join(name);

    let mut options = std::fs::OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt as _;
        // The document carries a SAML request that names the user to the
        // identity provider; it is nobody else's business on a shared machine.
        options.mode(0o600);
    }

    let mut file = options
        .open(&path)
        .map_err(|error| auth_error(&format!("cannot prepare the browser sign-in: {error}")))?;
    file.write_all(html.as_bytes())
        .map_err(|error| auth_error(&format!("cannot prepare the browser sign-in: {error}")))?;

    Ok(path.to_string_lossy().into_owned())
}

#[cfg(test)]
mod handoff {
    use super::*;

    #[test]
    fn a_redirect_request_is_opened_as_it_stands() {
        let request = SamlRequest {
            method: Method::Redirect,
            payload: String::from("https://idp.example.com/saml"),
        };

        assert_eq!(
            browser_url(&request).ok().as_deref(),
            Some("https://idp.example.com/saml")
        );
    }

    #[test]
    fn a_redirect_to_something_that_is_not_a_web_address_is_refused() {
        for payload in ["file:///etc/passwd", "javascript:alert(1)", "not a url"] {
            let request = SamlRequest {
                method: Method::Redirect,
                payload: String::from(payload),
            };
            assert!(
                browser_url(&request).is_err(),
                "{payload} was handed to the browser"
            );
        }
    }

    #[test]
    fn a_post_request_becomes_a_private_file_the_browser_can_open() {
        let html = "<html><body onload=\"f.submit()\"></body></html>";
        let request = SamlRequest {
            method: Method::Post,
            payload: String::from(html),
        };

        let url = browser_url(&request).expect("a file url");
        let path = url.strip_prefix("file://").expect("a file url");

        assert_eq!(std::fs::read_to_string(path).unwrap_or_default(), html);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt as _;
            let mode = std::fs::metadata(path)
                .map(|meta| meta.permissions().mode() & 0o777)
                .unwrap_or_default();
            // It names the user to their identity provider.
            assert_eq!(mode, 0o600, "the SAML request was world-readable");
        }
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn a_callback_with_nothing_waiting_is_dropped() {
        // A re-opened tab or a second click, long after the sign-in finished.
        assert!(!deliver_callback("globalprotectcallback:stale"));
    }
}
