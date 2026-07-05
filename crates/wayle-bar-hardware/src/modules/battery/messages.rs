use std::{rc::Rc, sync::Arc};

use wayle_battery::BatteryService;
use wayle_config::{ConfigService, schemas::styling::ThresholdColors};
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub struct BatteryInit {
    pub settings: BarSettings,
    pub battery: Arc<BatteryService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum BatteryMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum BatteryCmd {
    UpdateLabel(String),
    UpdateIcon(String),
    UpdateThresholdColors(ThresholdColors),
}
