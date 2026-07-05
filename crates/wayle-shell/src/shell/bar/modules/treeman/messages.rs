use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_treeman::TreemanService;
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub(crate) struct TreemanInit {
    pub settings: BarSettings,
    pub treeman: Arc<TreemanService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub(crate) enum TreemanMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub(crate) enum TreemanCmd {
    /// New label text, icon, severity class, and module visibility.
    Update {
        label: String,
        icon: String,
        severity: Option<&'static str>,
        visible: bool,
    },
}
