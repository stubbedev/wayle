//! Signing in to a gateway that answers with an HTML login form.
//!
//! Juniper (`nc`), Pulse Connect Secure (`pulse`) and F5 BIG-IP (`f5`) all
//! authenticate the same way: fetch a login page, post the form back with a
//! username and password in it, possibly answer an interstitial page or two,
//! and end up with a session cookie. Only the endpoints and the cookie's name
//! differ, which is why they share this module rather than getting one each.
//!
//! # Why this reads the form instead of knowing it
//!
//! These gateways are *configured* by their administrators: the realm picker,
//! the second-factor page, the role-selection page and the "you already have a
//! session" confirmation are all optional, and the hidden fields carry
//! deployment-specific tokens. Hard-coding any one deployment's markup would
//! work exactly once.
//!
//! So nothing here knows the form. It reads every `<input>` on the page, keeps
//! the values already in them, fills in the ones that name a username or a
//! password, and posts the lot back to the form's own action. That is what a
//! browser does, and it is the only version of this that survives meeting a
//! second gateway.
//!
//! # What is and is not verified
//!
//! Pinned by the tests: the form and input parsing, which fields count as the
//! username and password, the interstitial detection, the cookie extraction,
//! and the resolution of relative form actions. These are the parts that
//! decide what gets posted where, and they are exercised against the markup
//! shapes openconnect's `auth-juniper.c` and `f5.c` describe.
//!
//! **Not** verified: any real gateway. Nobody has one of these to point this
//! at, and their markup is per-deployment by design. This is why sign-in for
//! these protocols is something a profile opts into (`wayle-signin`), and why
//! failing here falls back to the plugin's own auth dialog rather than
//! stopping the connection attempt.

#[cfg(test)]
use std::collections::HashMap;

use super::xml;
use crate::Error;

/// How many interstitial pages to work through before giving up.
///
/// A realm picker, a second factor and a confirmation is three; a gateway
/// still asking after this is looping.
const MAX_PAGES: usize = 6;

/// Input names that mean "the username", lowercased.
///
/// From the login pages openconnect's `auth-juniper.c` and `f5.c` handle.
const USERNAME_FIELDS: &[&str] = &["username", "user", "uname", "userid"];

/// Input names that mean "the password", lowercased.
const PASSWORD_FIELDS: &[&str] = &["password", "passwd", "pass", "password#2"];

/// Input types that are never posted back with the rest.
///
/// A submit button's value is only sent for the button actually clicked, and
/// sending every one of them at once tells the gateway to do several things.
/// `reset` does nothing on the wire, and `file` has no value to send.
const SKIPPED_TYPES: &[&str] = &["submit", "reset", "button", "image", "file"];

/// Every `<input …>` opening tag on the page.
///
/// `<input>` is a void element: HTML gives it no closing tag, so the XML
/// reader used everywhere else here finds none of them. Scanning for opening
/// tags is the whole requirement, since every value an input carries lives in
/// its attributes.
fn input_tags(html: &str) -> Vec<&str> {
    let mut found = Vec::new();
    let mut rest = html;

    while let Some(at) = find_tag(rest, "input") {
        let after = &rest[at..];
        let Some(end) = after.find('>') else {
            break;
        };
        found.push(&after[..=end]);
        rest = &after[end + 1..];
    }
    found
}

/// Where the next `<name` opening tag starts, matched case-insensitively and
/// only when it is the whole tag name.
fn find_tag(html: &str, name: &str) -> Option<usize> {
    let lower = html.to_ascii_lowercase();
    let needle = format!("<{name}");
    let mut from = 0;

    while let Some(at) = lower[from..].find(&needle) {
        let start = from + at;
        let next = lower[start + needle.len()..].chars().next();
        // `<input ` or `<input>`, not `<inputmode>`.
        if next.is_none_or(|c| c.is_whitespace() || c == '>' || c == '/') {
            return Some(start);
        }
        from = start + needle.len();
    }
    None
}

/// One HTML form, as much of it as signing in needs.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub(super) struct Form {
    /// Where to post it, exactly as written in the markup.
    pub action: Option<String>,
    /// Every input worth posting back, in document order.
    pub fields: Vec<(String, String)>,
}

impl Form {
    /// Whether this form is asking for a password.
    pub fn wants_password(&self) -> bool {
        self.fields
            .iter()
            .any(|(name, _)| PASSWORD_FIELDS.contains(&name.to_ascii_lowercase().as_str()))
    }

    /// Whether this form is asking who is signing in.
    pub fn wants_username(&self) -> bool {
        self.fields
            .iter()
            .any(|(name, _)| USERNAME_FIELDS.contains(&name.to_ascii_lowercase().as_str()))
    }

    /// Fills in the credentials this form asks for, leaving everything else —
    /// realm, tokens, hidden state — exactly as the gateway sent it.
    pub fn filled(&self, username: &str, password: &str) -> Vec<(String, String)> {
        self.fields
            .iter()
            .map(|(name, value)| {
                let lower = name.to_ascii_lowercase();
                if USERNAME_FIELDS.contains(&lower.as_str()) {
                    (name.clone(), String::from(username))
                } else if PASSWORD_FIELDS.contains(&lower.as_str()) {
                    (name.clone(), String::from(password))
                } else {
                    (name.clone(), value.clone())
                }
            })
            .collect()
    }
}

/// Reads the first form on a page.
///
/// Returns `None` when the page has no form, which is how a successful sign-in
/// is told from another question: the gateway stops asking.
pub(super) fn parse_form(html: &str) -> Option<Form> {
    let at = find_tag(html, "form")?;
    let element = &html[at..];
    let action = xml::attribute(element, "action").filter(|action| !action.is_empty());

    let fields = input_tags(element).into_iter().filter_map(field).collect();

    Some(Form { action, fields })
}

/// One input's name and value, or nothing when it is not worth posting.
fn field(input: &str) -> Option<(String, String)> {
    let name = xml::attribute(input, "name").filter(|name| !name.is_empty())?;
    let kind = xml::attribute(input, "type").unwrap_or_default();
    if SKIPPED_TYPES.contains(&kind.to_ascii_lowercase().as_str()) {
        return None;
    }
    // An unchecked box is not submitted; a checked one is. Anything the
    // gateway pre-ticked is part of the answer it expects back.
    if matches!(kind.to_ascii_lowercase().as_str(), "checkbox" | "radio")
        && xml::attribute(input, "checked").is_none()
        && !input.to_ascii_lowercase().contains(" checked")
    {
        return None;
    }

    Some((name, xml::attribute(input, "value").unwrap_or_default()))
}

/// Turns a form's `action` into an absolute URL.
///
/// Gateways write all three forms: absolute, root-relative, and relative to
/// the page the form came from. Posting a relative action to the wrong base is
/// a 404 the user would see as "wrong password".
pub(super) fn resolve_action(base: &str, action: Option<&str>) -> String {
    let Some(action) = action.map(str::trim).filter(|action| !action.is_empty()) else {
        // No action means "post back to where this came from".
        return String::from(base);
    };
    if action.starts_with("https://") || action.starts_with("http://") {
        return String::from(action);
    }

    let origin = origin_of(base);
    if action.starts_with('/') {
        return format!("{origin}{action}");
    }

    let directory = base
        .rfind('/')
        // Past `https://`, so the slash found is a path separator rather than
        // one of the scheme's own.
        .filter(|slash| *slash > origin.len())
        .map_or_else(
            || format!("{origin}/"),
            |slash| String::from(&base[..=slash]),
        );
    format!("{directory}{action}")
}

/// The `scheme://host` of a URL.
fn origin_of(url: &str) -> String {
    let after_scheme = url.find("://").map_or(0, |at| at + 3);
    let end = url[after_scheme..]
        .find('/')
        .map_or(url.len(), |at| after_scheme + at);
    String::from(&url[..end])
}

/// The session cookie a gateway sets, out of its `Set-Cookie` headers.
///
/// Matched by name because that is what identifies the session: `DSID` for
/// Juniper and Pulse, `MRHSession` for F5.
pub(super) fn session_cookie(headers: &[String], name: &str) -> Option<String> {
    headers.iter().find_map(|header| {
        let (key, value) = header.split_once('=')?;
        // A cookie header is `NAME=value; Path=/; Secure`; only the first pair
        // is the cookie itself.
        if !key.trim().eq_ignore_ascii_case(name) {
            return None;
        }
        let value = value.split(';').next().unwrap_or_default().trim();
        // A gateway clears a cookie by setting it empty; that is a sign-out,
        // not a session.
        (!value.is_empty() && value != "\"\"").then(|| value.to_owned())
    })
}

/// What a page turned out to be.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) enum Page {
    /// A form asking for the username and password.
    Credentials(Form),
    /// A form asking for something else — a realm, a second factor, a role, a
    /// confirmation — which is answered by posting it back as it came.
    Interstitial(Form),
    /// No form: the gateway has stopped asking.
    Done,
}

/// Classifies a page.
pub(super) fn classify(html: &str) -> Page {
    match parse_form(html) {
        Some(form) if form.wants_password() || form.wants_username() => Page::Credentials(form),
        Some(form) => Page::Interstitial(form),
        None => Page::Done,
    }
}

/// The most rounds [`classify`]-driven sign-in will play.
pub(super) const fn max_pages() -> usize {
    MAX_PAGES
}

/// Body for one post, form-encoded.
pub(super) fn body(fields: &[(String, String)]) -> String {
    let pairs: Vec<(&str, &str)> = fields
        .iter()
        .map(|(name, value)| (name.as_str(), value.as_str()))
        .collect();
    super::form::encode(&pairs)
}

/// Builds the openconnect `--cookie` string for these protocols, which is the
/// session cookie exactly as the gateway named it.
pub(super) fn cookie_string(name: &str, value: &str) -> String {
    super::form::encode(&[(name, value)])
}

pub(super) fn auth_error(message: &str) -> Error {
    Error::VpnAuthenticationFailed(String::from(message))
}

/// The fields a form carries, as a map, for tests.
#[cfg(test)]
fn as_map(fields: &[(String, String)]) -> HashMap<String, String> {
    fields.iter().cloned().collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A Juniper login page, in the shape `auth-juniper.c` describes: a realm
    /// picker, hidden deployment tokens, and the credentials.
    const JUNIPER_LOGIN: &str = r#"
        <html><body>
        <form name="frmLogin" method="post" action="/dana-na/auth/url_default/login.cgi">
          <input type="hidden" name="tz_offset" value="60">
          <input type="text" name="username" value="">
          <input type="password" name="password" value="">
          <input type="hidden" name="realm" value="Users">
          <input type="submit" name="btnSubmit" value="Sign In">
        </form></body></html>
    "#;

    /// The "you already have a session" page: a form, but not one asking who
    /// you are.
    const JUNIPER_CONFIRM: &str = r#"
        <html><body>
        <form name="frmConfirmation" method="post" action="login.cgi">
          <input type="hidden" name="btnContinue" value="Continue the session">
          <input type="hidden" name="FormDataStr" value="deployment-token">
        </form></body></html>
    "#;

    #[test]
    fn a_login_page_yields_its_fields_and_action() {
        let form = parse_form(JUNIPER_LOGIN).expect("a form");

        assert_eq!(
            form.action.as_deref(),
            Some("/dana-na/auth/url_default/login.cgi")
        );
        assert!(form.wants_username());
        assert!(form.wants_password());
        let fields = as_map(&form.fields);
        // Hidden deployment state has to survive: the gateway expects its own
        // tokens back.
        assert_eq!(fields.get("tz_offset").map(String::as_str), Some("60"));
        assert_eq!(fields.get("realm").map(String::as_str), Some("Users"));
    }

    #[test]
    fn a_submit_button_is_not_posted_with_everything_else() {
        let form = parse_form(JUNIPER_LOGIN).expect("a form");

        // Sending every button at once tells the gateway to do several things.
        assert!(!as_map(&form.fields).contains_key("btnSubmit"));
    }

    #[test]
    fn credentials_replace_only_the_fields_that_ask_for_them() {
        let form = parse_form(JUNIPER_LOGIN).expect("a form");

        let filled = as_map(&form.filled("alice", "hunter2"));

        assert_eq!(filled.get("username").map(String::as_str), Some("alice"));
        assert_eq!(filled.get("password").map(String::as_str), Some("hunter2"));
        assert_eq!(filled.get("realm").map(String::as_str), Some("Users"));
        assert_eq!(filled.get("tz_offset").map(String::as_str), Some("60"));
    }

    #[test]
    fn a_page_with_no_credentials_is_an_interstitial_not_a_login() {
        // Answered by posting it back unchanged, not by asking the user again.
        assert!(matches!(classify(JUNIPER_CONFIRM), Page::Interstitial(_)));
        assert!(matches!(classify(JUNIPER_LOGIN), Page::Credentials(_)));
    }

    #[test]
    fn a_page_with_no_form_means_the_gateway_stopped_asking() {
        assert_eq!(classify("<html><body>Welcome</body></html>"), Page::Done);
        assert_eq!(classify(""), Page::Done);
    }

    #[test]
    fn an_unchecked_box_is_not_submitted_and_a_checked_one_is() {
        let html = r#"<form action="/a">
            <input type="checkbox" name="remember" value="1">
            <input type="checkbox" name="agree" value="yes" checked>
        </form>"#;

        let fields = as_map(&parse_form(html).expect("a form").fields);

        assert!(!fields.contains_key("remember"));
        assert_eq!(fields.get("agree").map(String::as_str), Some("yes"));
    }

    #[test]
    fn an_input_with_no_name_has_nothing_to_post() {
        let html = r#"<form action="/a"><input type="text" value="x"></form>"#;

        assert!(parse_form(html).expect("a form").fields.is_empty());
    }

    #[test]
    fn relative_actions_resolve_against_the_page_they_came_from() {
        let base = "https://vpn.example.com/dana-na/auth/url_default/welcome.cgi";

        // Relative to the page's directory.
        assert_eq!(
            resolve_action(base, Some("login.cgi")),
            "https://vpn.example.com/dana-na/auth/url_default/login.cgi"
        );
        // Root-relative.
        assert_eq!(
            resolve_action(base, Some("/dana-na/auth/login.cgi")),
            "https://vpn.example.com/dana-na/auth/login.cgi"
        );
        // Already absolute.
        assert_eq!(
            resolve_action(base, Some("https://other.example.com/x")),
            "https://other.example.com/x"
        );
    }

    #[test]
    fn a_missing_action_posts_back_to_the_same_page() {
        let base = "https://vpn.example.com/my.policy";

        assert_eq!(resolve_action(base, None), base);
        assert_eq!(resolve_action(base, Some("   ")), base);
    }

    #[test]
    fn an_action_on_a_bare_host_still_resolves() {
        // No path to be relative to; the scheme's own slashes must not be
        // mistaken for a path separator.
        assert_eq!(
            resolve_action("https://vpn.example.com", Some("login.cgi")),
            "https://vpn.example.com/login.cgi"
        );
        assert_eq!(
            resolve_action("https://vpn.example.com", Some("/login.cgi")),
            "https://vpn.example.com/login.cgi"
        );
    }

    #[test]
    fn the_session_cookie_is_found_by_name() {
        let headers = vec![
            String::from("lastRealm=Users; path=/; secure"),
            String::from("DSID=abc123def; path=/; secure; HttpOnly"),
        ];

        assert_eq!(
            session_cookie(&headers, "DSID").as_deref(),
            Some("abc123def")
        );
        assert_eq!(
            session_cookie(&headers, "dsid").as_deref(),
            Some("abc123def"),
            "cookie names are matched case-insensitively"
        );
    }

    #[test]
    fn a_cleared_or_absent_cookie_is_not_a_session() {
        // Clearing a cookie is how a gateway signs you out; reading that as a
        // session would hand the plugin an empty cookie and fail obscurely.
        let cleared = vec![String::from("DSID=; path=/; expires=Thu, 01 Jan 1970")];
        assert_eq!(session_cookie(&cleared, "DSID"), None);
        assert_eq!(session_cookie(&[], "DSID"), None);
        assert_eq!(
            session_cookie(&[String::from("MRHSession=x")], "DSID"),
            None
        );
        // A different cookie whose value contains the name must not match.
        assert_eq!(
            session_cookie(&[String::from("other=DSID=x")], "DSID"),
            None
        );
    }

    #[test]
    fn the_cookie_string_is_what_openconnect_takes() {
        assert_eq!(cookie_string("DSID", "abc123"), "DSID=abc123");
        // Values are escaped: a cookie with a separator in it would otherwise
        // read as two.
        assert_eq!(cookie_string("MRHSession", "a&b"), "MRHSession=a%26b");
    }

    #[test]
    fn the_post_body_carries_every_field() {
        let body = body(&[
            (String::from("username"), String::from("alice")),
            (String::from("realm"), String::from("Users")),
        ]);

        assert_eq!(body, "username=alice&realm=Users");
    }

    #[test]
    fn there_is_a_bound_on_how_long_a_gateway_may_keep_asking() {
        assert!(max_pages() >= 3, "a realm, a factor and a confirmation");
        assert!(max_pages() < 20, "an unbounded loop is a hung sign-in");
    }
}
