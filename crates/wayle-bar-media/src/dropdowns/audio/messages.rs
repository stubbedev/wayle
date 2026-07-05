use std::sync::Arc;

use wayle_audio::AudioService;
use wayle_config::ConfigService;

use super::{device_picker::DevicePickerOutput, main_section::MainSectionOutput};

pub struct AudioDropdownInit {
    pub audio: Arc<AudioService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum AudioDropdownMsg {
    MainSection(MainSectionOutput),
    OutputPicker(DevicePickerOutput),
    InputPicker(DevicePickerOutput),
}

#[derive(Debug)]
pub enum AudioDropdownCmd {
    ScaleChanged(f32),
}
