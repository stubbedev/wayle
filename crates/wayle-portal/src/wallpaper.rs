//! `org.freedesktop.impl.portal.Wallpaper`.
//!
//! Maps `SetWallpaperURI` onto the running shell's `com.wayle.Wallpaper1`
//! service. Only `file://` URIs are supported (the shell sets a local image on
//! every monitor).

use std::collections::HashMap;

use tracing::warn;
use zbus::{
    Connection, interface, proxy,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::response::Response;

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
        _options: HashMap<String, OwnedValue>,
    ) -> u32 {
        let Some(path) = uri_to_path(&uri) else {
            warn!(%uri, "wallpaper: only file:// URIs are supported");
            return Response::Other.code();
        };
        let proxy = match WallpaperProxy::new(&self.connection).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "wallpaper: shell service unavailable");
                return Response::Other.code();
            }
        };
        // Empty monitor = all monitors.
        match proxy.set_wallpaper(&path, "").await {
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
