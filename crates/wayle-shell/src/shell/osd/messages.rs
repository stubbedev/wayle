use std::sync::Arc;

use wayle_audio::{
    AudioService,
    core::device::{input::InputDevice, output::OutputDevice},
};
use wayle_brightness::{BacklightDevice, BrightnessService};
use wayle_config::ConfigService;

pub(crate) struct OsdInit {
    pub(crate) config: Arc<ConfigService>,
    pub(crate) audio: Option<Arc<AudioService>>,
    pub(crate) brightness: Option<Arc<BrightnessService>>,
    pub(crate) toast_bus: crate::services::ToastBus,
}

#[derive(Debug, Clone)]
pub(crate) enum OsdEvent {
    Slider {
        label: String,
        icon: String,
        percentage: f64,
        muted: bool,
    },

    Toggle {
        label: String,
        icon: String,
        active: bool,
    },

    /// A user-defined toast pushed over the socket. Shows a progress bar when
    /// `percentage` is set, otherwise an icon + label.
    Custom {
        label: String,
        icon: Option<String>,
        percentage: Option<f64>,
        duration_ms: Option<u32>,
        /// Extra CSS class applied to the toast, from `--class` or a preset.
        class: Option<String>,
    },
}

#[derive(Debug)]
pub(crate) enum OsdCmd {
    Ready,
    Dismiss(u32),
    ConfigChanged,
    DeviceChanged(Option<Arc<OutputDevice>>),
    VolumeChanged,
    InputDeviceChanged(Option<Arc<InputDevice>>),
    InputVolumeChanged,
    BrightnessDevicesChanged(Vec<Arc<BacklightDevice>>),
    BrightnessChanged,
    ToggleChanged(ToggleEvent),
    ShowToast(crate::services::widget_ipc::ToastRequest),
    Hide(u32),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[allow(clippy::enum_variant_names)]
pub(crate) enum ToggleKey {
    CapsLock,
    NumLock,
    ScrollLock,
}

#[derive(Debug, Clone)]
pub(crate) struct ToggleEvent {
    pub(crate) key: ToggleKey,
    pub(crate) active: bool,
}
