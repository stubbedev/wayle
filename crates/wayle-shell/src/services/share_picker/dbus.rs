//! D-Bus interface for the share picker.

use tokio::sync::oneshot;
use tracing::{instrument, warn};
use wayle_share_preview::toplevel::Toplevel;
use zbus::interface;

use super::picker_sender;
use crate::shell::share_picker::SharePickerInput;

pub struct SharePickerDaemon;

#[interface(name = "com.wayle.SharePicker1")]
impl SharePickerDaemon {
    /// Shows the picker and blocks until the user selects or cancels.
    ///
    /// Returns the XDPH selection suffix (printed after `[SELECTION]`), or an
    /// empty string when cancelled or when the shell UI is not yet ready.
    #[instrument(skip(self))]
    pub async fn pick(&self, window_list: &str, allow_token: bool) -> String {
        let Some(sender) = picker_sender() else {
            warn!("share picker requested before the shell UI registered its sender");
            return String::new();
        };

        let toplevels = Toplevel::parse_list(window_list);
        let (reply_tx, reply_rx) = oneshot::channel();

        sender.emit(SharePickerInput::Show {
            toplevels,
            allow_token,
            reply: reply_tx,
        });

        reply_rx.await.unwrap_or_else(|_| {
            warn!("share picker reply channel dropped");
            String::new()
        })
    }
}
