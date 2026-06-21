//! `org.freedesktop.impl.portal.Secret`.
//!
//! Hands a sandboxed application a stable per-app master secret. libsecret's
//! portal backend uses it to derive the encryption key for the app's private
//! keyring file, so the secret must be identical across runs — otherwise that
//! keyring becomes permanently undecryptable.
//!
//! The secret is [`SECRET_LEN`] random bytes, generated on the first request
//! for an `app_id` and persisted at `$XDG_DATA_HOME/wayle/portal/secrets/<id>`
//! (mode `0600`). On each `RetrieveSecret` the bytes are written to the
//! caller-supplied writable fd. No keyring daemon is required, so this works on
//! every compositor.

use std::{
    collections::HashMap,
    fs,
    io::Write as _,
    os::unix::fs::{DirBuilderExt as _, OpenOptionsExt as _},
    path::PathBuf,
};

use rand::TryRngCore as _;
use tracing::warn;
use zbus::{
    interface,
    zvariant::{OwnedFd, OwnedObjectPath},
};

use crate::{dbus_util::Vardict, response::Response};

/// Length of a master secret, in bytes. Matches the 64-byte secrets the
/// reference backends generate.
const SECRET_LEN: usize = 64;

/// Secret portal interface.
pub struct Secret;

impl Secret {
    /// Builds the interface. Storage is the filesystem, so it holds no state.
    #[must_use]
    pub fn new() -> Self {
        Self
    }
}

#[interface(name = "org.freedesktop.impl.portal.Secret")]
impl Secret {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Retrieves (or, on first use, generates) the master secret for `app_id`
    /// and writes the raw bytes to `fd`.
    async fn retrieve_secret(
        &self,
        _handle: OwnedObjectPath,
        app_id: String,
        fd: OwnedFd,
        _options: Vardict,
    ) -> (u32, Vardict) {
        let secret = match load_or_create(&app_id) {
            Ok(bytes) => bytes,
            Err(err) => {
                warn!(%err, %app_id, "secret: could not obtain master secret");
                return (Response::Other.code(), HashMap::new());
            }
        };

        let raw: std::os::fd::OwnedFd = fd.into();
        let mut file = fs::File::from(raw);
        if let Err(err) = file.write_all(&secret) {
            warn!(%err, %app_id, "secret: writing to fd failed");
            return (Response::Other.code(), HashMap::new());
        }

        (Response::Success.code(), HashMap::new())
    }
}

/// Reads the persisted secret for `app_id`, generating and storing a fresh one
/// if none exists yet (or the stored file is the wrong length).
fn load_or_create(app_id: &str) -> std::io::Result<Vec<u8>> {
    let path = secret_path(app_id)?;

    if let Ok(bytes) = fs::read(&path)
        && bytes.len() == SECRET_LEN
    {
        return Ok(bytes);
    }

    let mut secret = vec![0u8; SECRET_LEN];
    rand::rngs::OsRng
        .try_fill_bytes(&mut secret)
        .map_err(std::io::Error::other)?;

    if let Some(parent) = path.parent() {
        fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(parent)?;
    }
    let mut file = fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(&path)?;
    file.write_all(&secret)?;

    Ok(secret)
}

/// The on-disk path for `app_id`'s secret, under
/// `$XDG_DATA_HOME/wayle/portal/secrets` (falling back to `~/.local/share`).
/// The `app_id` is sanitized so it can never escape the directory.
fn secret_path(app_id: &str) -> std::io::Result<PathBuf> {
    let base = std::env::var_os("XDG_DATA_HOME")
        .map(PathBuf::from)
        .filter(|p| p.is_absolute())
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".local/share")))
        .ok_or_else(|| std::io::Error::other("neither XDG_DATA_HOME nor HOME is set"))?;

    Ok(base
        .join("wayle/portal/secrets")
        .join(sanitize(app_id)))
}

/// Maps an `app_id` to a safe single-path-component filename: anything that is
/// not alphanumeric, `.`, `-`, or `_` becomes `_`, and a leading `.` is escaped
/// so the result is never `.`, `..`, empty, or a hidden traversal.
fn sanitize(app_id: &str) -> String {
    let mut out = String::with_capacity(app_id.len().max(1));
    for ch in app_id.chars() {
        if ch.is_ascii_alphanumeric() || matches!(ch, '.' | '-' | '_') {
            out.push(ch);
        } else {
            out.push('_');
        }
    }
    if out.is_empty() || out.starts_with('.') {
        out.insert(0, '_');
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_blocks_traversal() {
        assert_eq!(sanitize("../../etc/passwd"), "_.._.._etc_passwd");
        assert_eq!(sanitize(".."), "_..");
        assert_eq!(sanitize("."), "_.");
        assert_eq!(sanitize(""), "_");
        assert_eq!(sanitize("/"), "__");
    }

    #[test]
    fn sanitize_preserves_normal_app_ids() {
        assert_eq!(sanitize("org.gnome.Builder"), "org.gnome.Builder");
        assert_eq!(sanitize("com.example_app-1"), "com.example_app-1");
    }
}
