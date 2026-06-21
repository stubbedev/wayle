//! `org.freedesktop.impl.portal.FileChooser`.
//!
//! Delegates to the running shell's `com.wayle.FileChooser1`, which pops the
//! native `gtk::FileDialog` (GTK's own widget — not the xdg-desktop-portal-gtk
//! package). `OpenFile`/`SaveFile`/`SaveFiles` map onto open / save / pick-a-
//! folder.

use std::collections::HashMap;

use tracing::warn;
use wayle_ipc::file_chooser::FileChooserProxy;
use zbus::{Connection, interface, zvariant::OwnedObjectPath};

use crate::{
    dbus_util::{Vardict, opt_bool, opt_string, owned},
    response::Response,
};

/// FileChooser portal interface.
pub struct FileChooser {
    connection: Connection,
}

impl FileChooser {
    /// Builds the interface over the backend's session-bus connection.
    #[must_use]
    pub fn new(connection: Connection) -> Self {
        Self { connection }
    }

    async fn proxy(&self) -> Option<FileChooserProxy<'_>> {
        match FileChooserProxy::new(&self.connection).await {
            Ok(proxy) => Some(proxy),
            Err(err) => {
                warn!(%err, "filechooser: shell service unavailable (is the shell running?)");
                None
            }
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.FileChooser")]
impl FileChooser {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        3
    }

    /// Opens existing file(s) or a directory.
    async fn open_file(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        title: String,
        options: Vardict,
    ) -> (u32, Vardict) {
        let multiple = opt_bool(&options, "multiple").unwrap_or(false);
        let directory = opt_bool(&options, "directory").unwrap_or(false);

        let Some(proxy) = self.proxy().await else {
            return (Response::Other.code(), HashMap::new());
        };
        match proxy
            .open_file(&title, multiple, directory, filters(&options), &current_folder(&options))
            .await
        {
            Ok(uris) => uris_response(uris),
            Err(err) => {
                warn!(%err, "filechooser: open failed");
                (Response::Other.code(), HashMap::new())
            }
        }
    }

    /// Chooses a save destination.
    async fn save_file(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        title: String,
        options: Vardict,
    ) -> (u32, Vardict) {
        let current_name = opt_string(&options, "current_name").unwrap_or_default();

        let Some(proxy) = self.proxy().await else {
            return (Response::Other.code(), HashMap::new());
        };
        match proxy
            .save_file(&title, &current_name, filters(&options), &current_folder(&options))
            .await
        {
            Ok(uris) => uris_response(uris),
            Err(err) => {
                warn!(%err, "filechooser: save failed");
                (Response::Other.code(), HashMap::new())
            }
        }
    }

    /// Chooses a destination folder for multiple files; returns one URI per
    /// requested filename inside it.
    async fn save_files(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        title: String,
        options: Vardict,
    ) -> (u32, Vardict) {
        let Some(proxy) = self.proxy().await else {
            return (Response::Other.code(), HashMap::new());
        };
        let folder = match proxy
            .open_file(&title, false, true, Vec::new(), &current_folder(&options))
            .await
        {
            Ok(uris) => match uris.into_iter().next() {
                Some(folder) => folder,
                None => return (Response::Cancelled.code(), HashMap::new()),
            },
            Err(err) => {
                warn!(%err, "filechooser: folder pick failed");
                return (Response::Other.code(), HashMap::new());
            }
        };
        let uris: Vec<String> = file_names(&options)
            .into_iter()
            .map(|name| format!("{}/{name}", folder.trim_end_matches('/')))
            .collect();
        uris_response(uris)
    }
}

/// Decodes the `filters` option (`a(sa(us))`) into `(name, [(kind, value)])`.
fn filters(options: &Vardict) -> Vec<(String, Vec<(u32, String)>)> {
    options
        .get("filters")
        .and_then(|v| Vec::<(String, Vec<(u32, String)>)>::try_from(v.try_clone().ok()?).ok())
        .unwrap_or_default()
}

/// Decodes the `current_folder` option (`ay` — a NUL-terminated path).
fn current_folder(options: &Vardict) -> String {
    options
        .get("current_folder")
        .and_then(|v| Vec::<u8>::try_from(v.try_clone().ok()?).ok())
        .map(|bytes| {
            String::from_utf8_lossy(&bytes)
                .trim_end_matches('\0')
                .to_owned()
        })
        .unwrap_or_default()
}

/// Builds the `(response, results)` pair from chosen URIs (empty = cancelled).
fn uris_response(uris: Vec<String>) -> (u32, Vardict) {
    if uris.is_empty() {
        return (Response::Cancelled.code(), HashMap::new());
    }
    let mut results = HashMap::new();
    if let Some(value) = owned(uris) {
        results.insert("uris".to_owned(), value);
    }
    (Response::Success.code(), results)
}

/// Decodes the `files` option (`aay` — NUL-terminated byte-string names).
fn file_names(options: &Vardict) -> Vec<String> {
    options
        .get("files")
        .and_then(|v| Vec::<Vec<u8>>::try_from(v.try_clone().ok()?).ok())
        .unwrap_or_default()
        .into_iter()
        .map(|bytes| {
            String::from_utf8_lossy(&bytes)
                .trim_end_matches('\0')
                .to_owned()
        })
        .collect()
}
