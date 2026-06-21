//! `org.freedesktop.impl.portal.Print`.
//!
//! `PreparePrint` shows the native `GtkPrintUnixDialog` (via the shell's
//! `com.wayle.Print1`) and returns the chosen settings + a token; `Print`
//! spools the document fd to the prepared printer through `GtkPrintJob`. No
//! xdg-desktop-portal-gtk.

use std::collections::HashMap;

use tracing::warn;
use wayle_ipc::print::PrintProxy;
use zbus::{
    Connection, interface,
    zvariant::{OwnedFd, OwnedObjectPath, Value},
};

use crate::{
    dbus_util::{Vardict, opt_u32, owned},
    response::Response,
};

/// Print portal interface.
pub struct Print {
    connection: Connection,
}

impl Print {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }

    async fn proxy(&self) -> Option<PrintProxy<'_>> {
        match PrintProxy::new(&self.connection).await {
            Ok(proxy) => Some(proxy),
            Err(err) => {
                warn!(%err, "print: host unavailable");
                None
            }
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Print")]
impl Print {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Shows the print dialog and returns the chosen settings + a token.
    async fn prepare_print(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        title: String,
        _settings: Vardict,
        _page_setup: Vardict,
        _options: Vardict,
    ) -> (u32, Vardict) {
        let Some(proxy) = self.proxy().await else {
            return (Response::Other.code(), HashMap::new());
        };
        match proxy.prepare(&title).await {
            Ok((true, settings, token)) => {
                let mut results = HashMap::new();
                if let Some(value) = owned(settings_dict(settings)) {
                    results.insert("settings".to_owned(), value);
                }
                // Page setup is carried inside the GTK settings; return empty.
                if let Some(value) = owned(HashMap::<String, String>::new()) {
                    results.insert("page_setup".to_owned(), value);
                }
                if let Some(value) = owned(token) {
                    results.insert("token".to_owned(), value);
                }
                (Response::Success.code(), results)
            }
            Ok(_) => (Response::Cancelled.code(), HashMap::new()),
            Err(err) => {
                warn!(%err, "print: prepare failed");
                (Response::Other.code(), HashMap::new())
            }
        }
    }

    /// Spools the document fd to the prepared printer.
    async fn print(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        title: String,
        fd: OwnedFd,
        options: Vardict,
    ) -> (u32, Vardict) {
        let token = opt_u32(&options, "token").unwrap_or(0);
        let Some(proxy) = self.proxy().await else {
            return (Response::Other.code(), HashMap::new());
        };
        match proxy.print(&title, fd, token).await {
            Ok(true) => (Response::Success.code(), HashMap::new()),
            Ok(false) => (Response::Other.code(), HashMap::new()),
            Err(err) => {
                warn!(%err, "print: spooling failed");
                (Response::Other.code(), HashMap::new())
            }
        }
    }
}

/// Wraps flat GTK print-setting key/value pairs as an `a{sv}` of strings.
fn settings_dict(pairs: Vec<(String, String)>) -> HashMap<String, Value<'static>> {
    pairs
        .into_iter()
        .map(|(key, value)| (key, Value::from(value)))
        .collect()
}
