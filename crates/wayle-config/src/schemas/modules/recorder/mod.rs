mod types;

use schemars::schema_for;
pub use types::RecorderFormat;
use wayle_derive::wayle_config;

use crate::{
    ClickAction, ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::styling::{ColorValue, CssToken, Percentage},
};

/// Native screen recorder backed by a GStreamer pipeline.
///
/// Click the bar button to start/stop; the dropdown exposes the recording
/// options below. Controllable from the CLI / RPC socket:
/// `wayle recorder start|stop|toggle|pause|status`.
#[wayle_config(bar_button, i18n_prefix = "settings-modules-recorder")]
pub struct RecorderConfig {
    /// Icon when idle (not recording).
    #[serde(rename = "icon-idle")]
    #[default(String::from("ld-video-symbolic"))]
    pub icon_idle: ConfigProperty<String>,

    /// Icon while recording.
    #[serde(rename = "icon-recording")]
    #[default(String::from("ld-circle-dot-symbolic"))]
    pub icon_recording: ConfigProperty<String>,

    /// Icon while recording is paused.
    #[serde(rename = "icon-paused")]
    #[default(String::from("ld-circle-pause-symbolic"))]
    pub icon_paused: ConfigProperty<String>,

    /// Format string for the label.
    ///
    /// ## Placeholders
    ///
    /// - `{{ state }}` - Recorder state text (Idle, Recording, Paused)
    /// - `{{ elapsed }}` - Elapsed recording time (e.g., "01:23", "--" when idle)
    #[serde(rename = "format")]
    #[default(String::from("{{ elapsed }}"))]
    pub format: ConfigProperty<String>,

    /// Capture the microphone in the recording.
    #[serde(rename = "microphone")]
    #[default(false)]
    pub microphone: ConfigProperty<bool>,

    /// Microphone PipeWire/PulseAudio source name. Empty uses the default source.
    #[serde(rename = "microphone-device")]
    #[default(String::new())]
    pub microphone_device: ConfigProperty<String>,

    /// Capture desktop (system) audio in the recording.
    #[serde(rename = "system-audio")]
    #[default(true)]
    pub system_audio: ConfigProperty<bool>,

    /// Capture framerate in frames per second.
    #[serde(rename = "framerate")]
    #[default(60u32)]
    pub framerate: ConfigProperty<u32>,

    /// Overlay a webcam picture-in-picture frame into the recording.
    #[serde(rename = "webcam-enabled")]
    #[default(false)]
    pub webcam_enabled: ConfigProperty<bool>,

    /// Webcam V4L2 device path. Empty auto-selects the first camera.
    #[serde(rename = "webcam-device")]
    #[default(String::new())]
    pub webcam_device: ConfigProperty<String>,

    /// Webcam frame horizontal position, as a percentage of the free
    /// horizontal space (0 = flush left, 100 = flush right). Stored relative so
    /// it stays correct across monitors of different resolutions.
    #[serde(rename = "webcam-x")]
    #[default(Percentage::new(100))]
    pub webcam_x: ConfigProperty<Percentage>,

    /// Webcam frame vertical position, as a percentage of the free vertical
    /// space (0 = flush top, 100 = flush bottom). Stored relative so it stays
    /// correct across monitors of different resolutions.
    #[serde(rename = "webcam-y")]
    #[default(Percentage::new(100))]
    pub webcam_y: ConfigProperty<Percentage>,

    /// Webcam frame width as a percentage of the recording width.
    #[serde(rename = "webcam-size")]
    #[default(Percentage::new(20))]
    pub webcam_size: ConfigProperty<Percentage>,

    /// Output directory for recordings. Empty uses the XDG Videos directory.
    #[serde(rename = "output-directory")]
    #[default(String::new())]
    pub output_directory: ConfigProperty<String>,

    /// Container format / codec preset.
    #[serde(rename = "output-format")]
    #[default(RecorderFormat::default())]
    pub output_format: ConfigProperty<RecorderFormat>,

    /// Draw the mouse cursor in the recording.
    #[serde(rename = "show-cursor")]
    #[default(true)]
    pub show_cursor: ConfigProperty<bool>,

    /// Display border around button.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color token.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::Red))]
    pub border_color: ConfigProperty<ColorValue>,

    /// Display module icon.
    #[serde(rename = "icon-show")]
    #[default(true)]
    pub icon_show: ConfigProperty<bool>,

    /// Icon foreground color. Auto selects based on variant for contrast.
    #[serde(rename = "icon-color")]
    #[default(ColorValue::Auto)]
    pub icon_color: ConfigProperty<ColorValue>,

    /// Icon container background color token.
    #[serde(rename = "icon-bg-color")]
    #[default(ColorValue::Token(CssToken::Red))]
    pub icon_bg_color: ConfigProperty<ColorValue>,

    /// Display label.
    #[serde(rename = "label-show")]
    #[default(true)]
    pub label_show: ConfigProperty<bool>,

    /// Label text color token.
    #[serde(rename = "label-color")]
    #[default(ColorValue::Token(CssToken::Red))]
    pub label_color: ConfigProperty<ColorValue>,

    /// Max label characters before truncation with ellipsis. Set to 0 to disable.
    #[serde(rename = "label-max-length")]
    #[default(0)]
    pub label_max_length: ConfigProperty<u32>,

    /// Button background color token.
    #[serde(rename = "button-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub button_bg_color: ConfigProperty<ColorValue>,

    /// Action on left click. Default toggles recording.
    #[serde(rename = "left-click")]
    #[default(ClickAction::Shell(String::from("wayle recorder toggle")))]
    pub left_click: ConfigProperty<ClickAction>,

    /// Action on right click. Default opens the recorder dropdown.
    #[serde(rename = "right-click")]
    #[default(ClickAction::Dropdown(String::from("recorder")))]
    pub right_click: ConfigProperty<ClickAction>,

    /// Action on middle click.
    #[serde(rename = "middle-click")]
    #[default(ClickAction::None)]
    pub middle_click: ConfigProperty<ClickAction>,

    /// Action on scroll up.
    #[serde(rename = "scroll-up")]
    #[default(ClickAction::None)]
    pub scroll_up: ConfigProperty<ClickAction>,

    /// Action on scroll down.
    #[serde(rename = "scroll-down")]
    #[default(ClickAction::None)]
    pub scroll_down: ConfigProperty<ClickAction>,
}

impl ModuleInfoProvider for RecorderConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("recorder"),
            schema: || schema_for!(RecorderConfig),
            layout_id: Some(String::from("recorder")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::bar_button()
    }
}

crate::register_module!(RecorderConfig);
