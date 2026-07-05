mod types;

#[cfg(feature = "schema")]
use schemars::schema_for;
pub use types::SharePickerPage;
use wayle_derive::wayle_config;

#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{ConfigProperty, schemas::styling::Size};

/// Base sizes (in rem, `1rem = 16px`) a `Size::Scale` multiplier resolves
/// against, so a scale value reads as a multiplier of the default (`Scale(1.0)`
/// = default). Shared by the shell resolver and the settings editor's scale↔px
/// conversion. (62.5rem = 1000px, etc.)
pub const WIDTH_BASE_REM: f32 = 62.5;
/// See [`WIDTH_BASE_REM`]. 31.25rem = 500px.
pub const HEIGHT_BASE_REM: f32 = 31.25;
/// See [`WIDTH_BASE_REM`]. 9.375rem = 150px.
pub const WIDGET_BASE_REM: f32 = 9.375;
/// See [`WIDTH_BASE_REM`]. 0.75rem = 12px.
pub const WINDOWS_SPACING_BASE_REM: f32 = 0.75;
/// See [`WIDTH_BASE_REM`]. 0.375rem = 6px.
pub const OUTPUTS_SPACING_BASE_REM: f32 = 0.375;

/// Screen-share picker shown by xdg-desktop-portal when an app requests a
/// window, output, or region to capture.
#[wayle_config(i18n_prefix = "settings-share-picker")]
pub struct SharePickerConfig {
    /// Page selected when the picker opens.
    #[serde(rename = "default-page")]
    #[default(SharePickerPage::default())]
    pub default_page: ConfigProperty<SharePickerPage>,

    /// Hide the "allow a restore token" checkbox.
    #[serde(rename = "hide-token-restore")]
    #[default(false)]
    pub hide_token_restore: ConfigProperty<bool>,

    /// Picker window width: a multiplier of the default 1000px (`1.0` = default)
    /// or absolute pixels (e.g. `"1200px"`).
    #[default(Size::scale(1.0))]
    pub width: ConfigProperty<Size>,

    /// Picker window height: a multiplier of the default 500px (`1.0` = default)
    /// or absolute pixels.
    #[default(Size::scale(1.0))]
    pub height: ConfigProperty<Size>,

    /// Downscale every captured frame to at most this height in pixels.
    #[serde(rename = "resize-size")]
    #[default(640u32)]
    pub resize_size: ConfigProperty<u32>,

    /// Height of each card's preview image: a multiplier of the default 150px
    /// (`1.0` = default) or absolute pixels.
    #[serde(rename = "widget-size")]
    #[default(Size::scale(1.0))]
    pub widget_size: ConfigProperty<Size>,

    /// Spacing between window cards: a multiplier of the default 12px
    /// (`1.0` = default) or absolute pixels.
    #[serde(rename = "windows-spacing")]
    #[default(Size::scale(1.0))]
    pub windows_spacing: ConfigProperty<Size>,

    /// Minimum window cards per row.
    #[serde(rename = "windows-min-per-row")]
    #[default(3u32)]
    pub windows_min_per_row: ConfigProperty<u32>,

    /// Maximum window cards per row.
    #[serde(rename = "windows-max-per-row")]
    #[default(4u32)]
    pub windows_max_per_row: ConfigProperty<u32>,

    /// Spacing between output cards (applied per side): a multiplier of the
    /// default 6px (`1.0` = default) or absolute pixels.
    #[serde(rename = "outputs-spacing")]
    #[default(Size::scale(1.0))]
    pub outputs_spacing: ConfigProperty<Size>,

    /// Show the output name label under each output card.
    #[serde(rename = "outputs-show-label")]
    #[default(false)]
    pub outputs_show_label: ConfigProperty<bool>,

    /// Scale output cards by their fractional scale.
    #[serde(rename = "outputs-respect-scaling")]
    #[default(true)]
    pub outputs_respect_scaling: ConfigProperty<bool>,
}

#[cfg(feature = "schema")]
impl ModuleInfoProvider for SharePickerConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("share-picker"),
            schema: || schema_for!(SharePickerConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(SharePickerConfig);
