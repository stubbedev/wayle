use std::sync::Arc;

use wayle_audio::AudioService;
use wayle_config::ConfigService;

use super::default_devices::DefaultDevicesOutput;

pub struct MainSectionInit {
    pub audio: Arc<AudioService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum MainSectionInput {
    DefaultDevices(DefaultDevicesOutput),
}

#[derive(Debug)]
pub enum MainSectionOutput {
    ShowOutputDevices,
    ShowInputDevices,
}
