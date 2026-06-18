use std::sync::Arc;

use wayle_audio::AudioService;
use wayle_config::ConfigService;

use crate::services::recorder::RecorderState;

pub(crate) struct RecorderDropdownInit {
    pub config: Arc<ConfigService>,
    pub state: RecorderState,
    /// Audio service for enumerating microphone sources. `None` if audio is
    /// unavailable; the microphone-device picker then offers only "Default".
    pub audio: Option<Arc<AudioService>>,
}

#[derive(Debug)]
pub(crate) enum RecorderDropdownMsg {
    ToggleRecording,
    TogglePause,
    MicrophoneToggled(bool),
    MicrophoneDeviceSelected(u32),
    SystemAudioToggled(bool),
    WebcamToggled(bool),
    WebcamDeviceSelected(u32),
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
    /// The set of microphone sources changed (device hotplug); rebuild the
    /// microphone-device picker.
    MicrophonesUpdated,
}
