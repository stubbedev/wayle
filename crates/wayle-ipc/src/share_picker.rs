//! D-Bus client proxy for the share picker service.
//!
//! The `wayle share-picker` stub (invoked by xdg-desktop-portal-hyprland)
//! calls [`SharePickerProxy::pick`] to ask the running shell to display the
//! picker surface and return the user's selection.
#![allow(missing_docs)]

use zbus::{Result, proxy};

pub const SERVICE_NAME: &str = "com.wayle.SharePicker1";
pub const SERVICE_PATH: &str = "/com/wayle/SharePicker";

#[proxy(
    interface = "com.wayle.SharePicker1",
    default_service = "com.wayle.SharePicker1",
    default_path = "/com/wayle/SharePicker",
    gen_blocking = false
)]
pub trait SharePicker {
    /// Shows the picker for a portal request and returns the XDPH selection
    /// suffix (the part printed after `[SELECTION]`): e.g. `r/window:123`,
    /// `/screen:DP-1`, `/region:DP-1@0,0,800,600`. When `multiple` is set the
    /// user can pick several sources, returned as newline-separated payloads
    /// after the flag segment. Returns an empty string if the user cancels.
    ///
    /// `window_list` is the raw `XDPH_WINDOW_SHARING_LIST` value;
    /// `allow_token` seeds the restore-token checkbox; `multiple` enables
    /// multi-source selection.
    async fn pick(&self, window_list: &str, allow_token: bool, multiple: bool)
    -> Result<String>;
}
