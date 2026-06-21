//! D-Bus interface for the file chooser service.

use tokio::sync::oneshot;
use tracing::{instrument, warn};
use zbus::interface;

use super::host_sender;
use crate::shell::file_chooser::FileChooserInput;

pub struct FileChooserDaemon;

#[interface(name = "com.wayle.FileChooser1")]
impl FileChooserDaemon {
    /// Opens existing file(s) or a directory, returning the chosen `file://`
    /// URIs (empty list on cancel).
    #[instrument(skip(self))]
    pub async fn open_file(
        &self,
        title: &str,
        multiple: bool,
        directory: bool,
        filters: Vec<(String, Vec<(u32, String)>)>,
        current_folder: &str,
    ) -> zbus::fdo::Result<Vec<String>> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(FileChooserInput::Open {
            title: title.to_owned(),
            multiple,
            directory,
            filters,
            current_folder: current_folder.to_owned(),
            reply: reply_tx,
        })?;
        reply_rx.await.map_err(|_| dropped())
    }

    /// Chooses a save destination, returning the chosen `file://` URI.
    #[instrument(skip(self))]
    pub async fn save_file(
        &self,
        title: &str,
        current_name: &str,
        filters: Vec<(String, Vec<(u32, String)>)>,
        current_folder: &str,
    ) -> zbus::fdo::Result<Vec<String>> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(FileChooserInput::Save {
            title: title.to_owned(),
            current_name: current_name.to_owned(),
            filters,
            current_folder: current_folder.to_owned(),
            reply: reply_tx,
        })?;
        reply_rx.await.map_err(|_| dropped())
    }
}

impl FileChooserDaemon {
    /// Sends a request to the GTK-thread host.
    fn dispatch(&self, input: FileChooserInput) -> zbus::fdo::Result<()> {
        let Some(sender) = host_sender() else {
            warn!("file chooser requested before the shell UI registered its sender");
            return Err(zbus::fdo::Error::Failed("shell UI not ready".to_owned()));
        };
        sender.emit(input);
        Ok(())
    }
}

/// Error when the host drops the reply channel.
fn dropped() -> zbus::fdo::Error {
    warn!("file chooser reply channel dropped");
    zbus::fdo::Error::Failed("file chooser host unavailable".to_owned())
}
