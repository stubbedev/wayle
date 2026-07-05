use std::sync::Arc;

use wayle_audio::AudioService;

pub struct ControlsInit {
    pub audio: Option<Arc<AudioService>>,
}

#[derive(Debug)]
pub enum ControlsInput {
    VolumeCommitted(f64),
    MuteToggled,
}

#[derive(Debug)]
pub enum ControlsCmd {
    VolumeChanged(f64),
    MuteChanged(bool),
    DeviceNameChanged(String),
    DeviceAvailable(bool),
}
