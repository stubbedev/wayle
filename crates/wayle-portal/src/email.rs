//! `org.freedesktop.impl.portal.Email`.
//!
//! `ComposeEmail` launches the user's mail client at a pre-filled compose
//! window. When the request carries attachment fds we drive `xdg-email
//! --attach` (copying each fd to a temp file first); otherwise — and as a
//! fallback when `xdg-email` is absent — we build a `mailto:` URI and launch the
//! default handler via `xdg-open`. No GUI of our own; the mail client provides
//! the compose window.

use std::{
    collections::HashMap,
    io::{self, ErrorKind, Seek, SeekFrom},
    path::{Path, PathBuf},
    process::{Command, Stdio},
};

use tracing::warn;
use zbus::{
    interface,
    zvariant::{Fd, OwnedObjectPath, OwnedValue},
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
        // Attachments can't ride a `mailto:` URI — when present, drive
        // `xdg-email --attach` instead. Fall through to the mailto path if
        // xdg-email is missing or there are no attachments.
        let attachments = copy_attachment_fds(&options);
        if !attachments.is_empty() {
            match launch_xdg_email(&options, &attachments) {
                Ok(true) => return (Response::Success.code(), HashMap::new()),
                Ok(false) => warn!("email: xdg-email not found; attachments dropped"),
                Err(err) => warn!(%err, "email: xdg-email failed; falling back to mailto"),
            }
        }

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

/// Copies any `attachment_fds` to temp files, returning their paths. The fds
/// are only valid during the call, so we duplicate their contents up front.
/// Temp files are left for the mail client to read (the OS reaps `/tmp`).
fn copy_attachment_fds(options: &Vardict) -> Vec<PathBuf> {
    let Some(value) = options.get("attachment_fds") else {
        return Vec::new();
    };
    let fds: Vec<Fd> = match value.try_clone().ok().and_then(|v| Vec::try_from(v).ok()) {
        Some(fds) => fds,
        None => return Vec::new(),
    };

    let dir = std::env::temp_dir();
    let pid = std::process::id();
    fds.into_iter()
        .enumerate()
        .filter_map(|(i, fd)| {
            let path = dir.join(format!("wayle-email-{pid}-{i}"));
            copy_fd_to_file(fd, &path).then_some(path)
        })
        .collect()
}

/// Copies one attachment fd's contents into `path`. Returns whether it
/// succeeded; logs and returns `false` otherwise.
fn copy_fd_to_file(fd: Fd, path: &Path) -> bool {
    let std_fd = match std::os::fd::OwnedFd::try_from(fd) {
        Ok(fd) => fd,
        Err(err) => {
            warn!(%err, "email: cannot own attachment fd");
            return false;
        }
    };
    let mut src = std::fs::File::from(std_fd);
    let _ = src.seek(SeekFrom::Start(0));
    let mut dst = match std::fs::File::create(path) {
        Ok(dst) => dst,
        Err(err) => {
            warn!(%err, "email: cannot create temp attachment file");
            return false;
        }
    };
    if io::copy(&mut src, &mut dst).is_err() {
        warn!("email: cannot copy attachment fd");
        return false;
    }
    true
}

/// Launches `xdg-email` with the compose fields + `--attach` for each file.
/// Returns `Ok(false)` when `xdg-email` isn't installed (caller falls back to
/// the `mailto:` path).
fn launch_xdg_email(options: &Vardict, attachments: &[PathBuf]) -> io::Result<bool> {
    let mut cmd = Command::new("xdg-email");
    cmd.arg("--utf8");
    if let Some(subject) = opt_string(options, "subject") {
        cmd.arg("--subject").arg(subject);
    }
    if let Some(body) = opt_string(options, "body") {
        cmd.arg("--body").arg(body);
    }
    for cc in string_list(options, "cc") {
        cmd.arg("--cc").arg(cc);
    }
    for bcc in string_list(options, "bcc") {
        cmd.arg("--bcc").arg(bcc);
    }
    for path in attachments {
        cmd.arg("--attach").arg(path);
    }
    let mut to = string_list(options, "addresses");
    if let Some(single) = opt_string(options, "address") {
        to.push(single);
    }
    for addr in to {
        cmd.arg(addr);
    }

    match cmd.stdout(Stdio::null()).stderr(Stdio::null()).spawn() {
        Ok(mut child) => {
            std::thread::spawn(move || {
                let _ = child.wait();
            });
            Ok(true)
        }
        Err(err) if err.kind() == ErrorKind::NotFound => Ok(false),
        Err(err) => Err(err),
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
    use zbus::zvariant::Value;

    use super::*;

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
