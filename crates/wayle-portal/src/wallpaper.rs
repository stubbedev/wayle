//! `org.freedesktop.impl.portal.Wallpaper`.
//!
//! Maps `SetWallpaperURI` onto the running shell's `com.wayle.Wallpaper1`
//! service. Only `file://` URIs are supported (the shell sets a local image on
//! every monitor).

use std::collections::HashMap;

use tracing::warn;
use wayle_ipc::portal_dialogs::PortalDialogsProxy;
use zbus::{
    Connection, interface, proxy,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{dbus_util::opt_bool, response::Response};

/// Minimal client for the shell wallpaper service.
#[proxy(
    interface = "com.wayle.Wallpaper1",
    default_service = "com.wayle.Wallpaper1",
    default_path = "/com/wayle/Wallpaper",
    gen_blocking = false
)]
trait Wallpaper {
    async fn set_wallpaper(&self, path: &str, monitor: &str) -> zbus::Result<()>;
}

/// Wallpaper portal interface.
pub struct WallpaperPortal {
    connection: Connection,
}

impl WallpaperPortal {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }

    /// Runs the `show-preview` confirm dialog. Returns `Ok(())` to proceed with
    /// applying the wallpaper, or `Err(response_code)` to end the request now
    /// (the user declined, or the prompt failed). A missing dialog host is
    /// non-fatal — we proceed and apply directly.
    async fn confirm_preview(&self, uri: &str) -> Result<(), u32> {
        let dialogs = match PortalDialogsProxy::new(&self.connection).await {
            Ok(dialogs) => dialogs,
            Err(err) => {
                warn!(%err, "wallpaper: dialog host unavailable, applying directly");
                return Ok(());
            }
        };
        match dialogs.confirm_wallpaper(uri).await {
            Ok(true) => Ok(()),
            Ok(false) => Err(Response::Cancelled.code()),
            Err(err) => {
                warn!(%err, "wallpaper: preview prompt failed");
                Err(Response::Other.code())
            }
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Wallpaper")]
impl WallpaperPortal {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Sets the desktop wallpaper from a `file://` URI.
    async fn set_wallpaper_uri(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        uri: String,
        options: HashMap<String, OwnedValue>,
    ) -> u32 {
        let Some(path) = uri_to_path(&uri) else {
            warn!(%uri, "wallpaper: only file:// URIs are supported");
            return Response::Other.code();
        };

        // Honour `show-preview`: ask the user to confirm before applying.
        if opt_bool(&options, "show-preview").unwrap_or(false)
            && let Err(code) = self.confirm_preview(&uri).await
        {
            return code;
        }

        self.apply(&path).await
    }
}

impl WallpaperPortal {
    /// Sends the resolved path to the shell wallpaper service (empty monitor =
    /// all monitors), mapping the outcome to a portal response code.
    async fn apply(&self, path: &str) -> u32 {
        let proxy = match WallpaperProxy::new(&self.connection).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "wallpaper: shell service unavailable");
                return Response::Other.code();
            }
        };
        match proxy.set_wallpaper(path, "").await {
            Ok(()) => Response::Success.code(),
            Err(err) => {
                warn!(%err, "wallpaper: set_wallpaper failed");
                Response::Other.code()
            }
        }
    }
}

/// Decodes a `file://` URI into a filesystem path, undoing percent-encoding.
/// Returns `None` for non-`file` schemes.
fn uri_to_path(uri: &str) -> Option<String> {
    let rest = uri.strip_prefix("file://")?;
    // Drop an optional authority component (`file://host/path`).
    let path = match rest.find('/') {
        Some(0) => rest,
        Some(slash) => &rest[slash..],
        None => return None,
    };
    Some(percent_decode(path))
}

/// Decodes `%XX` escapes in a path.
fn percent_decode(input: &str) -> String {
    let bytes = input.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%'
            && i + 2 < bytes.len()
            && let Ok(byte) = u8::from_str_radix(&input[i + 1..i + 3], 16)
        {
            out.push(byte);
            i += 3;
            continue;
        }
        out.push(bytes[i]);
        i += 1;
    }
    String::from_utf8_lossy(&out).into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_plain_file_uri() {
        assert_eq!(
            uri_to_path("file:///home/u/bg.png").as_deref(),
            Some("/home/u/bg.png")
        );
    }

    #[test]
    fn decodes_percent_escapes() {
        assert_eq!(
            uri_to_path("file:///home/u/My%20Wallpapers/a%23b.png").as_deref(),
            Some("/home/u/My Wallpapers/a#b.png")
        );
    }

    #[test]
    fn rejects_non_file_scheme() {
        assert_eq!(uri_to_path("https://example.com/x.png"), None);
    }
}
