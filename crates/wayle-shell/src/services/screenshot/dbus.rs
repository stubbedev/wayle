//! D-Bus interface for the screenshot service.

use tokio::sync::oneshot;
use tracing::{instrument, warn};
use zbus::interface;

use super::host_sender;
use crate::shell::screenshot::ScreenshotInput;

pub struct ScreenshotDaemon;

#[interface(name = "com.wayle.Screenshot1")]
impl ScreenshotDaemon {
    /// Captures a screenshot and blocks until it is saved or cancelled.
    ///
    /// `mode` is `region`, `output`, or `window`; `target` is an optional
    /// output connector name (used by `output` mode). Returns the saved PNG
    /// path, or an empty string when the user cancels a region selection.
    #[instrument(skip(self))]
    pub async fn capture(&self, mode: &str, target: &str) -> zbus::fdo::Result<String> {
        let Some(sender) = host_sender() else {
            warn!("screenshot requested before the shell UI registered its sender");
            return Err(zbus::fdo::Error::Failed("shell UI not ready".to_owned()));
        };

        let (reply_tx, reply_rx) = oneshot::channel();
        sender.emit(ScreenshotInput::Capture {
            mode: mode.to_owned(),
            target: target.to_owned(),
            reply: reply_tx,
        });

        match reply_rx.await {
            Ok(Ok(path)) => Ok(path),
            Ok(Err(err)) => Err(zbus::fdo::Error::Failed(err)),
            Err(_) => {
                warn!("screenshot reply channel dropped");
                Err(zbus::fdo::Error::Failed(
                    "screenshot host unavailable".to_owned(),
                ))
            }
        }
    }
}
