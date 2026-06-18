use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_widgets::prelude::BarSettings;

use crate::{services::MailService, shell::bar::dropdowns::DropdownRegistry};

pub(crate) struct MailInit {
    pub settings: BarSettings,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
    pub mail: Arc<MailService>,
}

#[derive(Debug)]
pub(crate) enum MailMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub(crate) enum MailCmd {
    /// Total unread changed (from the mail service).
    CountChanged(u32),
    /// Display-affecting config changed (format/icon/hide-when-zero).
    ConfigChanged,
}
