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
        Arc, Mutex, OnceLock,
        atomic::{AtomicBool, Ordering},
    },
};

use tracing::{debug, warn};
use zbus::{Connection, interface, object_server::SignalEmitter, zvariant::OwnedObjectPath};

/// Cleanup run when a session closes (stop streams, release devices, …).
type OnClose = Box<dyn Fn() + Send + Sync>;

/// Shared per-session state: the run-once cleanup and the flag that guards it.
/// Held both by the [`Session`] D-Bus object and by the global [`registry`], so
/// either the app's `Close()` or process shutdown can run cleanup exactly once.
struct SessionInner {
    closed: AtomicBool,
    on_close: OnClose,
}

impl SessionInner {
    /// Runs the cleanup the first time it is called; subsequent calls no-op.
    /// Returns `true` if this call performed the cleanup.
    fn close_once(&self) -> bool {
        if self.closed.swap(true, Ordering::SeqCst) {
            return false;
        }
        (self.on_close)();
        true
    }
}

/// Process-global map of live sessions keyed by object path, so shutdown can
/// stop every session's PipeWire loop / device grab without each portal having
/// to expose its private [`SessionStore`].
fn registry() -> &'static Mutex<HashMap<OwnedObjectPath, Arc<SessionInner>>> {
    static REGISTRY: OnceLock<Mutex<HashMap<OwnedObjectPath, Arc<SessionInner>>>> = OnceLock::new();
    REGISTRY.get_or_init(|| Mutex::new(HashMap::new()))
}

/// Locks a poisoned-tolerant mutex, recovering the inner value if a holder
/// panicked rather than poisoning the registry forever.
fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|e| e.into_inner())
}

/// The Session D-Bus object.
struct Session {
    inner: Arc<SessionInner>,
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
        if !self.inner.close_once() {
            return;
        }
        debug!("portal session closed");
        let path: OwnedObjectPath = emitter.path().to_owned().into();
        lock(registry()).remove(&path);
        let _ = Session::closed(&emitter).await;
        // Await the removal directly: we are already async, and awaiting avoids
        // the handle-reuse race a fire-and-forget spawn would leave open.
        if let Err(err) = connection
            .object_server()
            .remove::<Session, _>(&path)
            .await
        {
            warn!(%err, "cannot unmount session object");
        }
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
    let inner = Arc::new(SessionInner {
        closed: AtomicBool::new(false),
        on_close: Box::new(on_close),
    });
    connection
        .object_server()
        .at(
            path,
            Session {
                inner: inner.clone(),
            },
        )
        .await?;
    lock(registry()).insert(path.clone(), inner);
    Ok(())
}

/// Runs cleanup for every still-live session, stopping their PipeWire loops /
/// releasing their device grabs. Called on process shutdown; the D-Bus objects
/// are torn down with the connection, so this only runs the cleanup callbacks
/// (each guarded to fire at most once).
pub fn clear_all() {
    let sessions: Vec<Arc<SessionInner>> = lock(registry()).drain().map(|(_, v)| v).collect();
    if !sessions.is_empty() {
        debug!(count = sessions.len(), "clearing live portal sessions");
    }
    for session in sessions {
        session.close_once();
    }
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
        lock(&self.inner).insert(path, value);
    }

    /// Returns a clone of the state for `path`, if present.
    #[must_use]
    pub fn get(&self, path: &OwnedObjectPath) -> Option<T> {
        lock(&self.inner).get(path).cloned()
    }

    /// Removes and returns the state for `path`.
    pub fn remove(&self, path: &OwnedObjectPath) -> Option<T> {
        lock(&self.inner).remove(path)
    }

    /// Mutates the state for `path` in place, if present.
    pub fn update<F: FnOnce(&mut T)>(&self, path: &OwnedObjectPath, f: F) {
        if let Some(value) = lock(&self.inner).get_mut(path) {
            f(value);
        }
    }
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::AtomicUsize;

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

    #[test]
    fn close_once_runs_cleanup_exactly_once() {
        let calls = Arc::new(AtomicUsize::new(0));
        let inner = {
            let calls = calls.clone();
            SessionInner {
                closed: AtomicBool::new(false),
                on_close: Box::new(move || {
                    calls.fetch_add(1, Ordering::SeqCst);
                }),
            }
        };
        assert!(inner.close_once());
        assert!(!inner.close_once());
        assert!(!inner.close_once());
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn store_recovers_from_poisoned_lock() {
        let store: SessionStore<u32> = SessionStore::default();
        let p = path("/session/poison");
        store.insert(p.clone(), 1);

        // Poison the mutex by panicking while holding the lock.
        let inner = store.inner.clone();
        let _ = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            let _guard = inner.lock().unwrap();
            panic!("poison the lock");
        }));
        assert!(store.inner.is_poisoned());

        // Subsequent operations still work rather than no-opping forever.
        assert_eq!(store.get(&p), Some(1));
        store.update(&p, |v| *v += 4);
        assert_eq!(store.remove(&p), Some(5));
    }
}
