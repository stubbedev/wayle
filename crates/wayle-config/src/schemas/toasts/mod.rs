mod types;

use schemars::schema_for;
pub use types::ToastPreset;
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::{
        general::Layer,
        osd::{OsdMonitor, OsdPosition, OsdTextAlign},
        styling::Size,
    },
};

/// Toast overlays shown via `wayle toast`.
///
/// Toasts are independent of the OSD: they have their own screen position,
/// monitor, layer, duration, alignment, and a list of reusable presets.
#[wayle_config(i18n_prefix = "settings-toasts")]
pub struct ToastsConfig {
    /// Show toast overlays pushed via `wayle toast`.
    #[default(true)]
    pub enabled: ConfigProperty<bool>,

    /// Screen anchor position.
    #[default(OsdPosition::default())]
    pub position: ConfigProperty<OsdPosition>,

    /// Horizontal alignment of toast content.
    #[serde(rename = "text-align")]
    #[default(OsdTextAlign::default())]
    pub text_align: ConfigProperty<OsdTextAlign>,

    /// Auto-dismiss delay in milliseconds.
    #[default(2500u32)]
    pub duration: ConfigProperty<u32>,

    /// Target monitor: "primary" or a connector name like "DP-1".
    #[default(OsdMonitor::default())]
    pub monitor: ConfigProperty<OsdMonitor>,

    /// Margin from screen edges. Accepts a scale multiplier or pixels (e.g.
    /// `"150px"`).
    #[default(Size::scale(150.0))]
    pub margin: ConfigProperty<Size>,

    /// Show a border around the toast.
    #[default(true)]
    pub border: ConfigProperty<bool>,

    /// Layer-shell layer toasts are placed on.
    #[default(Layer::Overlay)]
    pub layer: ConfigProperty<Layer>,

    /// Reusable toast presets, each triggerable with `wayle toast --preset <id>`.
    #[default(Vec::new())]
    pub presets: ConfigProperty<Vec<ToastPreset>>,
}

impl ModuleInfoProvider for ToastsConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("toasts"),
            schema: || schema_for!(ToastsConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

crate::register_module!(ToastsConfig);
