use std::{rc::Rc, sync::Arc};

use wayle_brightness::{BacklightDevice, BrightnessService};
use wayle_config::{ConfigService, schemas::styling::ThresholdColors};
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub struct BrightnessInit {
    pub settings: BarSettings,
    pub brightness: Arc<BrightnessService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum BrightnessMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub enum BrightnessCmd {
    DevicesChanged(Vec<Arc<BacklightDevice>>),
    BrightnessChanged,
    ConfigChanged,
    UpdateThresholdColors(ThresholdColors),
}
