//! D-Bus client proxy for the shell's print host.
//!
//! Backs the portal backend's `org.freedesktop.impl.portal.Print` with a native
//! `GtkPrintUnixDialog` + `GtkPrintJob` (GTK widgets, not xdg-desktop-portal-gtk).
#![allow(missing_docs)]

use zbus::{Result, proxy, zvariant::OwnedFd};

pub const SERVICE_NAME: &str = "com.wayle.Print1";
pub const SERVICE_PATH: &str = "/com/wayle/Print";

#[proxy(
    interface = "com.wayle.Print1",
    default_service = "com.wayle.Print1",
    default_path = "/com/wayle/Print",
    gen_blocking = false
)]
pub trait Print {
    /// Shows the print dialog so the user picks a printer + settings, stashing
    /// the selection under a returned token. Returns `(granted, settings, token)`
    /// where `settings` are flat GTK print-setting key/value pairs.
    async fn prepare(&self, title: &str) -> Result<(bool, Vec<(String, String)>, u32)>;

    /// Spools `document` (a PDF fd) to the printer prepared under `token`.
    /// Returns `true` if the job was sent.
    async fn print(&self, title: &str, document: OwnedFd, token: u32) -> Result<bool>;
}
