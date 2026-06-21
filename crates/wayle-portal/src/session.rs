//! `org.freedesktop.impl.portal.Session` plus a small per-interface session
//! store.
//!
//! Session-based interfaces (ScreenCast, RemoteDesktop, GlobalShortcuts) create
//! a Session object at `session_handle` in `CreateSession` that lives until the
//! app or the backend closes it. The backend exports a [`Session`] object there
//! and tracks per-session state in a [`SessionStore`].

use std::{
    collections::HashMap,
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
};

use tracing::debug;
use zbus::{Connection, interface, object_server::SignalEmitter, zvariant::OwnedObjectPath};

/// Cleanup run when a session closes (stop streams, release devices, …).
type OnClose = Box<dyn Fn() + Send + Sync>;

/// The Session D-Bus object.
struct Session {
    closed: Arc<AtomicBool>,
    on_close: OnClose,
}

#[interface(name = "org.freedesktop.impl.portal.Session")]
impl Session {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        2
    }

    /// Closes the session: runs cleanup, emits `Closed`, and unmounts.
    async fn close(
        &self,
        #[zbus(signal_emitter)] emitter: SignalEmitter<'_>,
        #[zbus(connection)] connection: &Connection,
    ) {
        if self.closed.swap(true, Ordering::SeqCst) {
            return;
        }
        debug!("portal session closed");
        (self.on_close)();
        let _ = Session::closed(&emitter).await;
        unmount(connection, emitter.path().to_owned().into());
    }

    /// Emitted when the backend closes the session.
    #[zbus(signal)]
    async fn closed(emitter: &SignalEmitter<'_>) -> zbus::Result<()>;
}

/// Mounts a Session object at `path`, invoking `on_close` exactly once when it
/// is closed (by the app's `Close()` or by [`close`]).
///
/// # Errors
///
/// Returns an error if the object cannot be registered.
pub async fn mount<F>(
    connection: &Connection,
    path: &OwnedObjectPath,
    on_close: F,
) -> zbus::Result<()>
where
    F: Fn() + Send + Sync + 'static,
{
    connection
        .object_server()
        .at(
            path,
            Session {
                closed: Arc::new(AtomicBool::new(false)),
                on_close: Box::new(on_close),
            },
        )
        .await?;
    Ok(())
}

/// Removes the Session object at `path`. Detached so it is safe to call from
/// within a Session method.
fn unmount(connection: &Connection, path: OwnedObjectPath) {
    let connection = connection.clone();
    tokio::spawn(async move {
        let _ = connection.object_server().remove::<Session, _>(&path).await;
    });
}

/// Thread-safe map of per-session state keyed by the session object path.
#[derive(Clone)]
pub struct SessionStore<T> {
    inner: Arc<Mutex<HashMap<OwnedObjectPath, T>>>,
}

impl<T> Default for SessionStore<T> {
    fn default() -> Self {
        Self {
            inner: Arc::new(Mutex::new(HashMap::new())),
        }
    }
}

impl<T: Clone> SessionStore<T> {
    /// Inserts or replaces the state for `path`.
    pub fn insert(&self, path: OwnedObjectPath, value: T) {
        if let Ok(mut map) = self.inner.lock() {
            map.insert(path, value);
        }
    }

    /// Returns a clone of the state for `path`, if present.
    #[must_use]
    pub fn get(&self, path: &OwnedObjectPath) -> Option<T> {
        self.inner.lock().ok()?.get(path).cloned()
    }

    /// Removes and returns the state for `path`.
    pub fn remove(&self, path: &OwnedObjectPath) -> Option<T> {
        self.inner.lock().ok()?.remove(path)
    }

    /// Mutates the state for `path` in place, if present.
    pub fn update<F: FnOnce(&mut T)>(&self, path: &OwnedObjectPath, f: F) {
        if let Ok(mut map) = self.inner.lock()
            && let Some(value) = map.get_mut(path)
        {
            f(value);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn path(s: &str) -> OwnedObjectPath {
        OwnedObjectPath::try_from(s).unwrap()
    }

    #[test]
    fn store_insert_get_remove() {
        let store: SessionStore<u32> = SessionStore::default();
        let p = path("/session/1");
        store.insert(p.clone(), 7);
        assert_eq!(store.get(&p), Some(7));
        store.update(&p, |v| *v += 1);
        assert_eq!(store.get(&p), Some(8));
        assert_eq!(store.remove(&p), Some(8));
        assert_eq!(store.get(&p), None);
    }
}
