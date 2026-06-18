use std::sync::Arc;

use relm4::ComponentSender;
use wayle_config::ConfigService;
use wayle_widgets::watch;

use super::{MailDropdown, messages::MailDropdownCmd};
use crate::services::MailService;

pub(super) fn spawn(
    sender: &ComponentSender<MailDropdown>,
    config: &Arc<ConfigService>,
    mail: &Arc<MailService>,
) {
    let scale = config.config().styling.scale.clone();
    watch!(sender, [scale.watch()], |out| {
        let _ = out.send(MailDropdownCmd::ScaleChanged(scale.get().value()));
    });

    let accounts = mail.accounts.clone();
    watch!(sender, [accounts.watch()], |out| {
        let _ = out.send(MailDropdownCmd::AccountsChanged(accounts.get()));
    });
}
