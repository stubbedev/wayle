use std::{rc::Rc, sync::Arc};

use wayle_config::{ConfigService, schemas::styling::ThresholdColors};
use wayle_sysinfo::SysinfoService;
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub struct CpuInit {
    pub settings: BarSettings,
    pub sysinfo: Arc<SysinfoService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum CpuMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum CpuCmd {
    UpdateLabel(String),
    UpdateIcon(String),
    UpdateThresholdColors(ThresholdColors),
}
