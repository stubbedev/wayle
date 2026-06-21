//! D-Bus client proxy for the file chooser service.
//!
//! The portal backend's `org.freedesktop.impl.portal.FileChooser` forwards to
//! the running shell, which pops the native `gtk::FileDialog`.
#![allow(missing_docs)]

use zbus::{Result, proxy};

pub const SERVICE_NAME: &str = "com.wayle.FileChooser1";
pub const SERVICE_PATH: &str = "/com/wayle/FileChooser";

#[proxy(
    interface = "com.wayle.FileChooser1",
    default_service = "com.wayle.FileChooser1",
    default_path = "/com/wayle/FileChooser",
    gen_blocking = false
)]
pub trait FileChooser {
    /// Opens existing file(s) or a directory. Returns the chosen `file://`
    /// URIs, or an empty list if the user cancelled.
    async fn open_file(&self, title: &str, multiple: bool, directory: bool) -> Result<Vec<String>>;

    /// Chooses a save destination seeded with `current_name`. Returns the
    /// chosen `file://` URI (single-element list), or empty on cancel.
    async fn save_file(&self, title: &str, current_name: &str) -> Result<Vec<String>>;
}
