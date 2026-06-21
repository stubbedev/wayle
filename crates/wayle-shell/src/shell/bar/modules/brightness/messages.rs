use std::{rc::Rc, sync::Arc};

use wayle_brightness::{BacklightDevice, BrightnessService};
use wayle_config::{ConfigService, schemas::styling::ThresholdColors};
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub(crate) struct BrightnessInit {
    pub settings: BarSettings,
    pub brightness: Arc<BrightnessService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub(crate) enum BrightnessMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub(crate) enum BrightnessCmd {
    DevicesChanged(Vec<Arc<BacklightDevice>>),
    BrightnessChanged,
    ConfigChanged,
    UpdateThresholdColors(ThresholdColors),
}
