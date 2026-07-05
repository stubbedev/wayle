use std::{rc::Rc, sync::Arc};

use wayle_audio::{AudioService, core::device::input::InputDevice};
use wayle_config::{ConfigService, schemas::styling::ThresholdColors};
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub struct MicrophoneInit {
    pub settings: BarSettings,
    pub audio: Arc<AudioService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum MicrophoneMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum MicrophoneCmd {
    DeviceChanged(Option<Arc<InputDevice>>),
    VolumeOrMuteChanged,
    IconConfigChanged,
    UpdateThresholdColors(ThresholdColors),
}
