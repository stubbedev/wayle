//! D-Bus client proxy for the screenshot service.
#![allow(missing_docs)]

use zbus::{Result, proxy};

pub const SERVICE_NAME: &str = "com.wayle.Screenshot1";
pub const SERVICE_PATH: &str = "/com/wayle/Screenshot";

#[proxy(
    interface = "com.wayle.Screenshot1",
    default_service = "com.wayle.Screenshot1",
    default_path = "/com/wayle/Screenshot",
    gen_blocking = false
)]
pub trait Screenshot {
    /// Captures a screenshot.
    ///
    /// `mode` is one of `region`, `output`, or `window`. `target` is an
    /// optional output connector name (used by `output` mode) or empty.
    /// Returns the saved PNG path, or an empty string if the user cancelled.
    async fn capture(&self, mode: &str, target: &str) -> Result<String>;
}
