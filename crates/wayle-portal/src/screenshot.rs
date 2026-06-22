//! `org.freedesktop.impl.portal.Screenshot`.
//!
//! Delegates to the running shell's `com.wayle.Screenshot1` service (the same
//! one the `wayle screenshot` CLI uses): `Screenshot` runs a region selection
//! (interactive) or grabs the whole multi-monitor screen (non-interactive,
//! matching xdg-desktop-portal-hyprland's `grim` whole-screen grab), and
//! `PickColor` runs the picker and samples one pixel. The shell owns all the
//! GTK overlay UI; this interface is a thin bridge plus the portal's result
//! encoding.

use std::collections::HashMap;

use tracing::warn;
use wayle_ipc::screenshot::ScreenshotProxy;
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{
    dbus_util::{opt_bool, owned},
    response::Response,
};

/// Screenshot portal interface.
pub struct Screenshot {
    connection: Connection,
}

impl Screenshot {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }

    async fn proxy(&self) -> Option<ScreenshotProxy<'_>> {
        match ScreenshotProxy::new(&self.connection).await {
            Ok(proxy) => Some(proxy),
            Err(err) => {
                warn!(%err, "screenshot: shell service unavailable (is the shell running?)");
                None
            }
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Screenshot")]
impl Screenshot {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        2
    }

    /// Takes a screenshot, returning its `file://` URI.
    async fn screenshot(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let interactive = opt_bool(&options, "interactive").unwrap_or(false);
        let mode = screenshot_mode(interactive);

        let Some(proxy) = self.proxy().await else {
            return (Response::Other.code(), HashMap::new());
        };
        match proxy.capture(mode, "").await {
            Ok(path) if !path.is_empty() => match owned(path_to_uri(&path)) {
                Some(uri) => (
                    Response::Success.code(),
                    HashMap::from([("uri".to_owned(), uri)]),
                ),
                None => (Response::Other.code(), HashMap::new()),
            },
            // Empty path = the user cancelled the selection.
            Ok(_) => (Response::Cancelled.code(), HashMap::new()),
            Err(err) => {
                warn!(%err, "screenshot: capture failed");
                (Response::Other.code(), HashMap::new())
            }
        }
    }

    /// Picks a screen color, returning it as an sRGB `(ddd)` tuple.
    async fn pick_color(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let Some(proxy) = self.proxy().await else {
            return (Response::Other.code(), HashMap::new());
        };
        match proxy.pick_color().await {
            Ok((r, g, b)) => match owned((r, g, b)) {
                Some(color) => (
                    Response::Success.code(),
                    HashMap::from([("color".to_owned(), color)]),
                ),
                None => (Response::Other.code(), HashMap::new()),
            },
            Err(err) => {
                warn!(%err, "screenshot: color pick failed/cancelled");
                (Response::Cancelled.code(), HashMap::new())
            }
        }
    }
}

/// The shell capture mode for an interactive vs. whole-screen request.
/// Non-interactive captures the entire compositor space across all outputs
/// (like `grim` with no geometry), not a single output.
fn screenshot_mode(interactive: bool) -> &'static str {
    if interactive { "region" } else { "screen" }
}

/// Converts an absolute filesystem path to a `file://` URI, percent-encoding
/// the bytes that are not allowed unescaped in a path.
fn path_to_uri(path: &str) -> String {
    let mut uri = String::from("file://");
    for byte in path.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' | b'/' => {
                uri.push(byte as char);
            }
            _ => uri.push_str(&format!("%{byte:02X}")),
        }
    }
    uri
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mode_selection() {
        assert_eq!(screenshot_mode(true), "region");
        assert_eq!(screenshot_mode(false), "screen");
    }

    #[test]
    fn uri_encodes_spaces_and_specials() {
        assert_eq!(
            path_to_uri("/home/u/Pictures/Screenshot 1.png"),
            "file:///home/u/Pictures/Screenshot%201.png"
        );
        assert_eq!(path_to_uri("/tmp/a#b.png"), "file:///tmp/a%23b.png");
    }

    #[test]
    fn uri_preserves_plain_path() {
        assert_eq!(path_to_uri("/tmp/shot.png"), "file:///tmp/shot.png");
    }
}
