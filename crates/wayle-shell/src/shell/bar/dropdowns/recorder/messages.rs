use std::sync::Arc;

use wayle_config::ConfigService;

use crate::services::recorder::RecorderState;

pub(crate) struct RecorderDropdownInit {
    pub config: Arc<ConfigService>,
    pub state: RecorderState,
}

#[derive(Debug)]
pub(crate) enum RecorderDropdownMsg {
    ToggleRecording,
    TogglePause,
    MicrophoneToggled(bool),
    SystemAudioToggled(bool),
    WebcamToggled(bool),
    PositionSelected(u32),
    BitrateChanged(u32),
    AudioBitrateChanged(u32),
    SeparateTracksToggled(bool),
    PresetSelected(u32),
}

#[derive(Debug)]
pub(crate) enum RecorderDropdownCmd {
    StateChanged,
    ScaleChanged(f32),
}
