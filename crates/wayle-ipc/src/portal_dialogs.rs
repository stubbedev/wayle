//! D-Bus client proxy for the shell's portal dialog host.
//!
//! Backs the portal backend's Access / Account / AppChooser / DynamicLauncher
//! interfaces with native shell dialogs (GTK widgets, not xdg-desktop-portal-gtk).
#![allow(missing_docs)]

use zbus::{Result, proxy};

pub const SERVICE_NAME: &str = "com.wayle.PortalDialogs1";
pub const SERVICE_PATH: &str = "/com/wayle/PortalDialogs";

#[proxy(
    interface = "com.wayle.PortalDialogs1",
    default_service = "com.wayle.PortalDialogs1",
    default_path = "/com/wayle/PortalDialogs",
    gen_blocking = false
)]
pub trait PortalDialogs {
    /// Generic grant/deny access prompt. Returns `true` if granted.
    async fn access(
        &self,
        title: &str,
        subtitle: &str,
        body: &str,
        grant_label: &str,
        deny_label: &str,
    ) -> Result<bool>;

    /// Confirms sharing the user's account info. Returns `true` if shared.
    async fn account(&self, reason: &str) -> Result<bool>;

    /// Picks an application to handle `uri`/`content_type`. `choices` are
    /// candidate desktop-file ids (empty = offer all). Returns the chosen
    /// desktop-file id, or empty string on cancel.
    async fn choose_application(
        &self,
        choices: Vec<&str>,
        content_type: &str,
        uri: &str,
    ) -> Result<String>;

    /// Confirms installing a dynamic launcher named `name`. Returns `true` if
    /// the user approved.
    async fn confirm_install(&self, name: &str, icon_name: &str) -> Result<bool>;
}
