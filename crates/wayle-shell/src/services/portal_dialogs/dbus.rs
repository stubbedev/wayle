//! D-Bus interface for the portal dialog service.

use tokio::sync::oneshot;
use tracing::{instrument, warn};
use zbus::interface;

use super::host_sender;
use crate::shell::portal_dialogs::PortalDialogInput;

pub struct PortalDialogsDaemon;

#[interface(name = "com.wayle.PortalDialogs1")]
impl PortalDialogsDaemon {
    /// Grant/deny prompt.
    #[instrument(skip(self))]
    pub async fn access(
        &self,
        title: &str,
        subtitle: &str,
        body: &str,
        grant_label: &str,
        deny_label: &str,
    ) -> zbus::fdo::Result<bool> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(PortalDialogInput::Access {
            title: title.to_owned(),
            subtitle: subtitle.to_owned(),
            body: body.to_owned(),
            grant_label: grant_label.to_owned(),
            deny_label: deny_label.to_owned(),
            reply: reply_tx,
        })?;
        reply_rx.await.map_err(|_| dropped())
    }

    /// Account-info consent.
    #[instrument(skip(self))]
    pub async fn account(&self, reason: &str) -> zbus::fdo::Result<bool> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(PortalDialogInput::Account {
            reason: reason.to_owned(),
            reply: reply_tx,
        })?;
        reply_rx.await.map_err(|_| dropped())
    }

    /// App chooser; returns the chosen desktop-file id (empty on cancel).
    #[instrument(skip(self))]
    pub async fn choose_application(
        &self,
        choices: Vec<String>,
        content_type: &str,
        uri: &str,
    ) -> zbus::fdo::Result<String> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(PortalDialogInput::ChooseApp {
            choices,
            content_type: content_type.to_owned(),
            uri: uri.to_owned(),
            reply: reply_tx,
        })?;
        reply_rx.await.map_err(|_| dropped())
    }

    /// Dynamic-launcher install confirmation.
    #[instrument(skip(self))]
    pub async fn confirm_install(&self, name: &str, _icon_name: &str) -> zbus::fdo::Result<bool> {
        let (reply_tx, reply_rx) = oneshot::channel();
        self.dispatch(PortalDialogInput::ConfirmInstall {
            name: name.to_owned(),
            reply: reply_tx,
        })?;
        reply_rx.await.map_err(|_| dropped())
    }
}

impl PortalDialogsDaemon {
    fn dispatch(&self, input: PortalDialogInput) -> zbus::fdo::Result<()> {
        let Some(sender) = host_sender() else {
            warn!("portal dialog requested before the shell UI registered its sender");
            return Err(zbus::fdo::Error::Failed("shell UI not ready".to_owned()));
        };
        sender.emit(input);
        Ok(())
    }
}

fn dropped() -> zbus::fdo::Error {
    warn!("portal dialog reply channel dropped");
    zbus::fdo::Error::Failed("portal dialog host unavailable".to_owned())
}
