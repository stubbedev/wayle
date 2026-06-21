//! D-Bus interface for the print service.

use std::os::fd::OwnedFd;

use tokio::sync::oneshot;
use tracing::{instrument, warn};
use zbus::interface;

use super::host_sender;
use crate::shell::print::{PrintInput, SettingsPairs};

pub struct PrintDaemon;

#[interface(name = "com.wayle.Print1")]
impl PrintDaemon {
    /// Shows the print dialog. Returns `(granted, settings, token)`.
    #[instrument(skip(self))]
    pub async fn prepare(&self, title: &str) -> zbus::fdo::Result<(bool, SettingsPairs, u32)> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(PrintInput::Prepare {
            title: title.to_owned(),
            reply: reply_tx,
        })?;
        match reply_rx.await.map_err(|_| dropped())? {
            Some((settings, token)) => Ok((true, settings, token)),
            None => Ok((false, Vec::new(), 0)),
        }
    }

    /// Spools `document` to the printer prepared under `token`.
    #[instrument(skip(self, document))]
    pub async fn print(
        &self,
        title: &str,
        document: zbus::zvariant::OwnedFd,
        token: u32,
    ) -> zbus::fdo::Result<bool> {
        let document = OwnedFd::from(document);
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(PrintInput::Spool {
            title: title.to_owned(),
            document,
            token,
            reply: reply_tx,
        })?;
        reply_rx.await.map_err(|_| dropped())
    }
}

impl PrintDaemon {
    fn dispatch(&self, input: PrintInput) -> zbus::fdo::Result<()> {
        let Some(sender) = host_sender() else {
            warn!("print requested before the shell UI registered its sender");
            return Err(zbus::fdo::Error::Failed("shell UI not ready".to_owned()));
        };
        sender.emit(input);
        Ok(())
    }
}

fn dropped() -> zbus::fdo::Error {
    warn!("print reply channel dropped");
    zbus::fdo::Error::Failed("print host unavailable".to_owned())
}
