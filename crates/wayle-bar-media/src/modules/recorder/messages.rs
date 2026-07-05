use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_widgets::prelude::BarSettings;

use crate::{services::recorder::RecorderService, shell::bar::dropdowns::DropdownRegistry};

pub struct RecorderInit {
    pub settings: BarSettings,
    pub recorder: Arc<RecorderService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum RecorderMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub enum RecorderCmd {
    ConfigChanged,
    StateChanged,
}
