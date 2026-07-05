use std::sync::Arc;

use wayle_audio::AudioService;

use crate::shell::bar::dropdowns::audio::VolumeSectionKind;

#[derive(Debug)]
pub struct DeviceInfo {
    pub description: String,
    pub subtitle: Option<String>,
    pub icon: &'static str,
    pub is_active: bool,
}

pub struct DevicePickerInit {
    pub audio: Arc<AudioService>,
    pub kind: VolumeSectionKind,
    pub title: String,
}

#[derive(Debug)]
pub enum DevicePickerInput {
    DeviceSelected(usize),
    BackClicked,
}

#[derive(Debug)]
pub enum DevicePickerCmd {
    DevicesChanged(Vec<DeviceInfo>),
}

#[derive(Debug)]
pub enum DevicePickerOutput {
    NavigateBack,
}
