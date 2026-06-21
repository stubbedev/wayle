//! `org.freedesktop.impl.portal.AppChooser`.
//!
//! Picks an application to handle a file/URI via the shell's native app-list
//! dialog (`com.wayle.PortalDialogs1`). No xdg-desktop-portal-gtk.

use std::collections::HashMap;

use tracing::warn;
use wayle_ipc::portal_dialogs::PortalDialogsProxy;
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{
    dbus_util::{opt_string, owned},
    response::Response,
};

/// AppChooser portal interface.
pub struct AppChooser {
    connection: Connection,
}

impl AppChooser {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }
}

#[interface(name = "org.freedesktop.impl.portal.AppChooser")]
impl AppChooser {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        2
    }

    /// Lets the user pick a handler application.
    async fn choose_application(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        choices: Vec<String>,
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let content_type = opt_string(&options, "content_type").unwrap_or_default();
        let uri = opt_string(&options, "uri").unwrap_or_default();

        let proxy = match PortalDialogsProxy::new(&self.connection).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "appchooser: dialog host unavailable");
                return (Response::Other.code(), HashMap::new());
            }
        };
        let choice_refs: Vec<&str> = choices.iter().map(String::as_str).collect();
        match proxy
            .choose_application(choice_refs, &content_type, &uri)
            .await
        {
            Ok(choice) if !choice.is_empty() => {
                let mut results = HashMap::new();
                if let Some(value) = owned(choice) {
                    results.insert("choice".to_owned(), value);
                }
                (Response::Success.code(), results)
            }
            Ok(_) => (Response::Cancelled.code(), HashMap::new()),
            Err(err) => {
                warn!(%err, "appchooser: selection failed");
                (Response::Other.code(), HashMap::new())
            }
        }
    }

    /// Updates the candidate list for an open chooser. We don't live-update the
    /// dialog; accept the call as a no-op.
    async fn update_choices(&self, _handle: OwnedObjectPath, _choices: Vec<String>) {}
}
