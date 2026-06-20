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
    /// Webcam frame dragged in the preview; carries the new relative position
    /// as percentages (0-100) of the free space.
    WebcamMoved { x_percent: u8, y_percent: u8 },
    /// Toggle the live on-screen webcam-position overlay.
    ToggleScreenPreview,
    /// The on-screen overlay asked to close (its "done" button), or it should
    /// be torn down (webcam disabled).
    ClosePreview,
}

#[derive(Debug)]
pub(crate) enum RecorderDropdownCmd {
    StateChanged,
    ScaleChanged(f32),
    /// The set of microphone sources changed (device hotplug); rebuild the
    /// microphone-device picker.
    MicrophonesUpdated,
}
