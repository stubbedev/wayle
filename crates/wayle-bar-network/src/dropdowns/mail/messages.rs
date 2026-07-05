use std::sync::Arc;

use wayle_config::ConfigService;

use crate::services::{MailService, mail::AccountUnread};

pub struct MailDropdownInit {
    pub config: Arc<ConfigService>,
    pub mail: Arc<MailService>,
}

#[derive(Debug)]
pub enum MailDropdownCmd {
    ScaleChanged(f32),
    AccountsChanged(Vec<AccountUnread>),
}
