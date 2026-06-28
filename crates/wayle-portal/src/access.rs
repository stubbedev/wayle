//! `org.freedesktop.impl.portal.Access`.
//!
//! Generic permission prompt, delegated to the shell's native dialog host
//! (`com.wayle.PortalDialogs1`) — a GTK `AlertDialog`, not xdg-desktop-portal-gtk.

use std::collections::HashMap;

use tracing::warn;
use wayle_ipc::portal_dialogs::PortalDialogsProxy;
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{dbus_util::opt_string, response::Response};

/// Access portal interface.
pub struct Access {
    connection: Connection,
}

impl Access {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Access")]
impl Access {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Shows a grant/deny prompt.
    async fn access_dialog(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        title: String,
        subtitle: String,
        body: String,
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let grant = opt_string(&options, "grant_label").unwrap_or_else(|| "Allow".to_owned());
        let deny = opt_string(&options, "deny_label").unwrap_or_else(|| "Deny".to_owned());
        let icon = opt_string(&options, "icon").unwrap_or_default();

        let proxy = match PortalDialogsProxy::new(&self.connection).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "access: dialog host unavailable");
                return (Response::Other.code(), HashMap::new());
            }
        };
        match proxy
            .access(&title, &subtitle, &body, &grant, &deny, &icon)
            .await
        {
            Ok(true) => (Response::Success.code(), HashMap::new()),
            Ok(false) => (Response::Cancelled.code(), HashMap::new()),
            Err(err) => {
                warn!(%err, "access: prompt failed");
                (Response::Other.code(), HashMap::new())
            }
        }
    }
}
