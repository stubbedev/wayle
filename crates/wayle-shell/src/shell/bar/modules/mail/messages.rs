use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub(crate) struct MailInit {
    pub settings: BarSettings,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
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
#[allow(clippy::enum_variant_names)]
pub(crate) enum MailCmd {
    /// Unread count changed (re-query result).
    CountChanged(u32),
    /// Display-affecting config changed (format/icon/hide-when-zero).
    ConfigChanged,
    /// The `query` config changed — re-query immediately.
    QueryChanged,
}
