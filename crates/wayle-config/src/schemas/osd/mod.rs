mod types;

#[cfg(feature = "schema")]
use schemars::schema_for;
pub use types::{OsdMonitor, OsdPosition, OsdTextAlign, ToastPreset};
use wayle_derive::wayle_config;

#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{
    ConfigProperty,
    schemas::{general::Layer, styling::Size},
};

/// Base size (in rem, `1rem = 16px`) the `margin` scale multiplier resolves
/// against (`Scale(1.0)` = default, 9.375rem = 150px). Shared by the shell
/// resolver and the settings scale↔px conversion.
pub const MARGIN_BASE_REM: f32 = 9.375;

/// On-screen display overlay for transient events like volume and brightness.
#[wayle_config(i18n_prefix = "settings-osd")]
pub struct OsdConfig {
    /// Show OSD overlays for volume, brightness, and keyboard toggles.
    #[default(true)]
    pub enabled: ConfigProperty<bool>,

    /// Screen anchor position.
    #[default(OsdPosition::default())]
    pub position: ConfigProperty<OsdPosition>,

    /// Horizontal alignment of toast and toggle overlay content. Sliders
    /// (volume/brightness) keep their own label+value layout.
    #[serde(rename = "text-align")]
    #[default(OsdTextAlign::default())]
    pub text_align: ConfigProperty<OsdTextAlign>,

    /// Auto-dismiss delay in milliseconds.
    #[default(2500u32)]
    pub duration: ConfigProperty<u32>,

    /// Target monitor: "primary" or a connector name like "DP-1".
    #[default(OsdMonitor::default())]
    pub monitor: ConfigProperty<OsdMonitor>,

    /// Margin from screen edges: a multiplier of the default 150px (`1.0` =
    /// default) or absolute pixels (e.g. `"150px"`).
    #[default(Size::scale(1.0))]
    pub margin: ConfigProperty<Size>,

    /// Show a border around the OSD.
    #[default(true)]
    pub border: ConfigProperty<bool>,

    /// Layer-shell layer the OSD is placed on.
    ///
    /// When `general.tearing-mode` is enabled, `overlay` is demoted to `top`
    /// to allow fullscreen tearing.
    #[default(Layer::Overlay)]
    pub layer: ConfigProperty<Layer>,

    /// Reusable toast presets, each triggerable with `wayle toast --preset <id>`.
    #[default(Vec::new())]
    pub presets: ConfigProperty<Vec<ToastPreset>>,
}

#[cfg(feature = "schema")]
impl ModuleInfoProvider for OsdConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("osd"),
            schema: || schema_for!(OsdConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(OsdConfig);
