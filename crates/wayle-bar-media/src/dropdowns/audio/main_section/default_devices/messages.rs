use std::sync::Arc;

use wayle_audio::AudioService;

use super::volume_section::VolumeSectionOutput;

pub struct DefaultDevicesInit {
    pub audio: Arc<AudioService>,
}

#[derive(Debug)]
pub enum DefaultDevicesInput {
    OutputSection(VolumeSectionOutput),
    InputSection(VolumeSectionOutput),
}

#[derive(Debug)]
pub enum DefaultDevicesOutput {
    ShowOutputDevices,
    ShowInputDevices,
    HasDeviceChanged(bool),
}
