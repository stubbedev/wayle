//! `org.freedesktop.impl.portal.DynamicLauncher`.
//!
//! Confirms installing a web/app launcher via the shell dialog host, then lets
//! the frontend write the `.desktop`. `RequestInstallToken` grants permission.
//! No xdg-desktop-portal-gtk.

use std::collections::HashMap;

use tracing::warn;
use wayle_ipc::portal_dialogs::PortalDialogsProxy;
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::response::Response;

/// Launcher types: 1 = Application, 2 = Web app.
const LAUNCHER_APPLICATION: u32 = 1;
const LAUNCHER_WEBAPP: u32 = 2;

/// DynamicLauncher portal interface.
pub struct DynamicLauncher {
    connection: Connection,
}

impl DynamicLauncher {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }
}

#[interface(name = "org.freedesktop.impl.portal.DynamicLauncher")]
impl DynamicLauncher {
    /// Launcher types we accept.
    #[zbus(property, name = "SupportedLauncherTypes")]
    fn supported_launcher_types(&self) -> u32 {
        LAUNCHER_APPLICATION | LAUNCHER_WEBAPP
    }

    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Confirms installing a launcher; echoes the (possibly user-approved)
    /// name + icon back to the frontend.
    async fn prepare_install(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        name: String,
        icon_v: OwnedValue,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let proxy = match PortalDialogsProxy::new(&self.connection).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "dynamiclauncher: dialog host unavailable");
                return (Response::Other.code(), HashMap::new());
            }
        };
        match proxy.confirm_install(&name, "").await {
            Ok(true) => {}
            Ok(false) => return (Response::Cancelled.code(), HashMap::new()),
            Err(err) => {
                warn!(%err, "dynamiclauncher: confirm failed");
                return (Response::Other.code(), HashMap::new());
            }
        }

        let mut results = HashMap::new();
        if let Ok(value) = OwnedValue::try_from(zbus::zvariant::Value::from(name)) {
            results.insert("name".to_owned(), value);
        }
        if let Ok(icon) = icon_v.try_clone() {
            results.insert("icon".to_owned(), icon);
        }
        (Response::Success.code(), results)
    }

    /// Grants permission to install a launcher (the frontend mints the token).
    async fn request_install_token(
        &self,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> u32 {
        Response::Success.code()
    }
}
