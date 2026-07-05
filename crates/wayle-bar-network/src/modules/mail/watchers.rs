use std::sync::Arc;

use relm4::ComponentSender;
use wayle_config::schemas::modules::MailConfig;
use wayle_widgets::watch;

use super::{MailModule, messages::MailCmd};
use crate::services::MailService;

pub fn spawn_config_watchers(sender: &ComponentSender<MailModule>, config: &MailConfig) {
    let format = config.format.clone();
    let icon_name = config.icon_name.clone();
    let hide_when_zero = config.hide_when_zero.clone();

    watch!(
        sender,
        [format.watch(), icon_name.watch(), hide_when_zero.watch()],
        |out| {
            let _ = out.send(MailCmd::ConfigChanged);
        }
    );
}

/// Bridge the shared mail service's total unread into the module's count.
pub fn spawn_total_watcher(sender: &ComponentSender<MailModule>, mail: &Arc<MailService>) {
    let total = mail.total.clone();
    watch!(sender, [total.watch()], |out| {
        let _ = out.send(MailCmd::CountChanged(total.get()));
    });
}
