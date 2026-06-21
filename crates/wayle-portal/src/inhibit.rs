//! `org.freedesktop.impl.portal.Inhibit`.
//!
//! Holds a systemd-logind inhibitor lock for the lifetime of the request. This
//! is the headless mechanism (no surface needed): `Inhibit` takes a logind lock
//! matching the requested flags and exports a Request object at `handle`; when
//! the app closes that request the lock fd is dropped and the inhibition ends.

use std::collections::HashMap;

use tracing::warn;
use zbus::{
    Connection, interface, proxy,
    zvariant::{OwnedFd, OwnedObjectPath, OwnedValue},
};

/// Inhibit flag bits (portal spec).
const FLAG_LOGOUT: u32 = 1;
const FLAG_SUSPEND: u32 = 4;
const FLAG_IDLE: u32 = 8;

/// Minimal client for the logind manager (system bus).
#[proxy(
    interface = "org.freedesktop.login1.Manager",
    default_service = "org.freedesktop.login1",
    default_path = "/org/freedesktop/login1",
    gen_blocking = false
)]
trait Logind {
    async fn inhibit(&self, what: &str, who: &str, why: &str, mode: &str) -> zbus::Result<OwnedFd>;
}

/// Inhibit portal interface.
pub struct Inhibit {
    connection: Connection,
}

impl Inhibit {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Inhibit")]
impl Inhibit {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Takes a logind inhibitor matching `flags` and exports a Request at
    /// `handle` that releases it on close.
    #[allow(clippy::cognitive_complexity)]
    async fn inhibit(
        &self,
        handle: OwnedObjectPath,
        _app_id: String,
        _window: String,
        flags: u32,
        _options: HashMap<String, OwnedValue>,
    ) {
        let what = inhibit_what(flags);
        if what.is_empty() {
            return;
        }

        let system = match Connection::system().await {
            Ok(connection) => connection,
            Err(err) => {
                warn!(%err, "inhibit: cannot reach the system bus");
                return;
            }
        };
        let proxy = match LogindProxy::new(&system).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "inhibit: logind unavailable");
                return;
            }
        };
        let fd = match proxy
            .inhibit(&what, "Wayle portal", "Application requested", "block")
            .await
        {
            Ok(fd) => fd,
            Err(err) => {
                warn!(%err, "inhibit: logind Inhibit failed");
                return;
            }
        };

        if let Err(err) = self
            .connection
            .object_server()
            .at(&handle, InhibitLock { _fd: fd })
            .await
        {
            warn!(%err, "inhibit: cannot export request object");
        }
    }
}

/// Request object whose lifetime holds the logind lock; closing it drops the fd.
struct InhibitLock {
    _fd: OwnedFd,
}

#[interface(name = "org.freedesktop.impl.portal.Request")]
impl InhibitLock {
    /// Releases the inhibition by unmounting (and thus dropping the fd).
    async fn close(
        &self,
        #[zbus(connection)] connection: &Connection,
        #[zbus(object_server)] _server: &zbus::ObjectServer,
        #[zbus(header)] header: zbus::message::Header<'_>,
    ) {
        if let Some(path) = header.path() {
            let path = path.to_owned();
            let connection = connection.clone();
            tokio::spawn(async move {
                let _ = connection
                    .object_server()
                    .remove::<InhibitLock, _>(&path)
                    .await;
            });
        }
    }
}

/// Builds the logind `what` string from portal inhibit flags.
fn inhibit_what(flags: u32) -> String {
    let mut what = Vec::new();
    if flags & FLAG_IDLE != 0 {
        what.push("idle");
    }
    if flags & FLAG_SUSPEND != 0 {
        what.push("sleep");
    }
    if flags & FLAG_LOGOUT != 0 {
        what.push("shutdown");
    }
    what.join(":")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_flags_to_logind_what() {
        assert_eq!(inhibit_what(0), "");
        assert_eq!(inhibit_what(FLAG_IDLE), "idle");
        assert_eq!(inhibit_what(FLAG_SUSPEND), "sleep");
        assert_eq!(inhibit_what(FLAG_LOGOUT), "shutdown");
        assert_eq!(
            inhibit_what(FLAG_IDLE | FLAG_SUSPEND | FLAG_LOGOUT),
            "idle:sleep:shutdown"
        );
    }
}
