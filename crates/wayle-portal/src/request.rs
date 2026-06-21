//! `org.freedesktop.impl.portal.Request`.
//!
//! The frontend passes a request `handle` (object path) into every interactive
//! method so the app can cancel an in-flight interaction via `Close()`. We
//! export a Request object there for the duration of the call ([`RequestGuard`])
//! and expose a [`Cancel`] the handler can race against.

use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

use tokio::sync::Notify;
use tracing::debug;
use zbus::{Connection, interface, zvariant::OwnedObjectPath};

/// Shared cancellation flag for an in-flight request.
#[derive(Clone, Default)]
pub struct Cancel {
    flag: Arc<AtomicBool>,
    notify: Arc<Notify>,
}

impl Cancel {
    /// Whether `Close()` has been called.
    #[must_use]
    pub fn is_cancelled(&self) -> bool {
        self.flag.load(Ordering::SeqCst)
    }

    /// Resolves as soon as the request is cancelled (immediately if already).
    pub async fn cancelled(&self) {
        if self.is_cancelled() {
            return;
        }
        self.notify.notified().await;
    }

    fn trip(&self) {
        self.flag.store(true, Ordering::SeqCst);
        self.notify.notify_waiters();
    }
}

/// The Request D-Bus object.
struct Request {
    cancel: Cancel,
}

#[interface(name = "org.freedesktop.impl.portal.Request")]
impl Request {
    /// Aborts the in-flight interaction this request belongs to.
    async fn close(&self) {
        debug!("portal request closed by caller");
        self.cancel.trip();
    }
}

/// Mounts a [`Request`] at `handle` and removes it on drop, so the object never
/// outlives the method call it guards.
pub struct RequestGuard {
    connection: Connection,
    handle: OwnedObjectPath,
    cancel: Cancel,
}

impl RequestGuard {
    /// Exports a Request object at `handle`.
    ///
    /// # Errors
    ///
    /// Returns an error if the object cannot be registered.
    pub async fn mount(connection: &Connection, handle: OwnedObjectPath) -> zbus::Result<Self> {
        let cancel = Cancel::default();
        connection
            .object_server()
            .at(&handle, Request { cancel: cancel.clone() })
            .await?;
        Ok(Self {
            connection: connection.clone(),
            handle,
            cancel,
        })
    }

    /// The cancellation handle for this request.
    #[must_use]
    pub fn cancel(&self) -> Cancel {
        self.cancel.clone()
    }
}

impl Drop for RequestGuard {
    fn drop(&mut self) {
        let connection = self.connection.clone();
        let handle = self.handle.clone();
        tokio::spawn(async move {
            let _ = connection.object_server().remove::<Request, _>(&handle).await;
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn cancel_resolves_after_trip() {
        let cancel = Cancel::default();
        assert!(!cancel.is_cancelled());
        let waiter = {
            let cancel = cancel.clone();
            tokio::spawn(async move { cancel.cancelled().await })
        };
        cancel.trip();
        assert!(cancel.is_cancelled());
        waiter.await.unwrap();
    }

    #[tokio::test]
    async fn cancelled_returns_immediately_when_already_tripped() {
        let cancel = Cancel::default();
        cancel.trip();
        cancel.cancelled().await;
    }
}
