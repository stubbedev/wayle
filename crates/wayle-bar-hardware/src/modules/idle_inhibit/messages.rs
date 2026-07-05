use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_widgets::prelude::BarSettings;

use crate::{services::idle_inhibit::IdleInhibitService, shell::bar::dropdowns::DropdownRegistry};

pub struct IdleInhibitInit {
    pub settings: BarSettings,
    pub idle_inhibit: Arc<IdleInhibitService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum IdleInhibitMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub enum IdleInhibitCmd {
    ConfigChanged,
    StateChanged,
}
