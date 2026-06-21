//! `org.freedesktop.impl.portal.Email`.
//!
//! `ComposeEmail` builds a `mailto:` URI from the requested recipients/subject/
//! body and launches the user's default mail handler via `xdg-open`. No GUI of
//! our own — the mail client provides the compose window. (Attachment fds are
//! not expressible in `mailto:` and are dropped, matching what most handlers
//! do.)

use std::collections::HashMap;
use std::process::{Command, Stdio};

use tracing::warn;
use zbus::{
    interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{
    dbus_util::{Vardict, opt_string},
    response::Response,
};

/// Email portal interface.
pub struct Email;

impl Email {
    /// Builds the interface.
    #[must_use]
    pub fn new() -> Self {
        Self
    }
}

impl Default for Email {
    fn default() -> Self {
        Self::new()
    }
}

#[interface(name = "org.freedesktop.impl.portal.Email")]
impl Email {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        3
    }

    /// Opens the user's mail client at a pre-filled compose window.
    async fn compose_email(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let uri = mailto(&options);
        match Command::new("xdg-open")
            .arg(&uri)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
        {
            Ok(mut child) => {
                // Reap on a detached thread; we don't block on the handler.
                std::thread::spawn(move || {
                    let _ = child.wait();
                });
                (Response::Success.code(), HashMap::new())
            }
            Err(err) => {
                warn!(%err, "email: cannot launch xdg-open");
                (Response::Other.code(), HashMap::new())
            }
        }
    }
}

/// Builds a `mailto:` URI from the compose options.
fn mailto(options: &Vardict) -> String {
    let mut to = string_list(options, "addresses");
    if let Some(single) = opt_string(options, "address") {
        to.push(single);
    }

    let mut uri = String::from("mailto:");
    uri.push_str(&to.join(","));

    let mut params: Vec<(&str, String)> = Vec::new();
    for (key, field) in [("cc", "cc"), ("bcc", "bcc")] {
        let list = string_list(options, field);
        if !list.is_empty() {
            params.push((key, list.join(",")));
        }
    }
    if let Some(subject) = opt_string(options, "subject") {
        params.push(("subject", subject));
    }
    if let Some(body) = opt_string(options, "body") {
        params.push(("body", body));
    }

    if !params.is_empty() {
        uri.push('?');
        let query: Vec<String> = params
            .into_iter()
            .map(|(key, value)| format!("{key}={}", encode(&value)))
            .collect();
        uri.push_str(&query.join("&"));
    }
    uri
}

/// Reads an `as` option into a `Vec<String>`.
fn string_list(options: &Vardict, key: &str) -> Vec<String> {
    options
        .get(key)
        .and_then(|v| Vec::<String>::try_from(v.try_clone().ok()?).ok())
        .unwrap_or_default()
}

/// Percent-encodes a query component (RFC 3986 unreserved kept literal).
fn encode(input: &str) -> String {
    let mut out = String::with_capacity(input.len());
    for byte in input.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char);
            }
            _ => out.push_str(&format!("%{byte:02X}")),
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use zbus::zvariant::Value;

    fn opts(pairs: &[(&str, Value<'static>)]) -> Vardict {
        pairs
            .iter()
            .map(|(k, v)| ((*k).to_owned(), OwnedValue::try_from(v.clone()).unwrap()))
            .collect()
    }

    #[test]
    fn builds_basic_mailto() {
        let o = opts(&[
            ("address", Value::from("a@b.com")),
            ("subject", Value::from("Hi there")),
            ("body", Value::from("Line & stuff")),
        ]);
        let uri = mailto(&o);
        assert!(uri.starts_with("mailto:a@b.com?"));
        assert!(uri.contains("subject=Hi%20there"));
        assert!(uri.contains("body=Line%20%26%20stuff"));
    }

    #[test]
    fn joins_multiple_addresses() {
        let o = opts(&[(
            "addresses",
            Value::from(vec!["x@y.com".to_string(), "z@w.com".to_string()]),
        )]);
        assert!(mailto(&o).starts_with("mailto:x@y.com,z@w.com"));
    }
}
