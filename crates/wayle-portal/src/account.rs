//! `org.freedesktop.impl.portal.Account`.
//!
//! Confirms sharing the user's account info via the shell dialog host, then
//! returns the local user's id / real name / avatar (read from passwd + the
//! `~/.face` icon). No xdg-desktop-portal-gtk involved.

use std::{collections::HashMap, ffi::CStr};

use tracing::warn;
use wayle_ipc::portal_dialogs::PortalDialogsProxy;
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{dbus_util::{opt_string, owned}, response::Response};

/// Account portal interface.
pub struct Account {
    connection: Connection,
}

impl Account {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Account")]
impl Account {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Returns the user's id, real name, and avatar after consent.
    async fn get_user_information(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _window: String,
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let reason = opt_string(&options, "reason").unwrap_or_default();

        let proxy = match PortalDialogsProxy::new(&self.connection).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "account: dialog host unavailable");
                return (Response::Other.code(), HashMap::new());
            }
        };
        match proxy.account(&reason).await {
            Ok(true) => {}
            Ok(false) => return (Response::Cancelled.code(), HashMap::new()),
            Err(err) => {
                warn!(%err, "account: prompt failed");
                return (Response::Other.code(), HashMap::new());
            }
        }

        let info = user_info();
        let mut results = HashMap::new();
        if let Some(value) = owned(info.id) {
            results.insert("id".to_owned(), value);
        }
        if let Some(value) = owned(info.name) {
            results.insert("name".to_owned(), value);
        }
        if !info.image.is_empty()
            && let Some(value) = owned(info.image)
        {
            results.insert("image".to_owned(), value);
        }
        (Response::Success.code(), results)
    }
}

/// Local user identity.
struct UserInfo {
    id: String,
    name: String,
    image: String,
}

/// Reads the local user's id (login), real name (passwd GECOS), and avatar
/// (`~/.face` as a `file://` URI, if present).
fn user_info() -> UserInfo {
    let mut id = std::env::var("USER").unwrap_or_default();
    let mut name = String::new();

    // SAFETY: getpwuid returns a pointer into a static buffer valid until the
    // next passwd call; we copy out immediately and make no further calls.
    unsafe {
        let pw = libc::getpwuid(libc::getuid());
        if !pw.is_null() {
            if id.is_empty() && !(*pw).pw_name.is_null() {
                id = CStr::from_ptr((*pw).pw_name).to_string_lossy().into_owned();
            }
            if !(*pw).pw_gecos.is_null() {
                let gecos = CStr::from_ptr((*pw).pw_gecos).to_string_lossy();
                name = gecos.split(',').next().unwrap_or("").trim().to_owned();
            }
        }
    }
    if name.is_empty() {
        name = id.clone();
    }

    let image = std::env::var_os("HOME")
        .map(std::path::PathBuf::from)
        .map(|home| home.join(".face"))
        .filter(|path| path.exists())
        .map(|path| format!("file://{}", path.to_string_lossy()))
        .unwrap_or_default();

    UserInfo { id, name, image }
}
