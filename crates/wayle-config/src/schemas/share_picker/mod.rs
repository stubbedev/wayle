mod types;

use schemars::schema_for;
pub use types::SharePickerPage;
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
};

/// Screen-share picker shown by xdg-desktop-portal when an app requests a
/// window, output, or region to capture.
#[wayle_config(i18n_prefix = "settings-share-picker")]
pub struct SharePickerConfig {
    /// Page selected when the picker opens.
    #[serde(rename = "default-page")]
    #[default(SharePickerPage::default())]
    pub default_page: ConfigProperty<SharePickerPage>,

    /// Clicks required to select a window/output card (1 = single, 2 = double).
    #[default(2u32)]
    pub clicks: ConfigProperty<u32>,

    /// Hide the "allow a restore token" checkbox.
    #[serde(rename = "hide-token-restore")]
    #[default(false)]
    pub hide_token_restore: ConfigProperty<bool>,

    /// Picker window width in pixels.
    #[default(1000u32)]
    pub width: ConfigProperty<u32>,

    /// Picker window height in pixels.
    #[default(500u32)]
    pub height: ConfigProperty<u32>,

    /// Downscale every captured frame to at most this height in pixels.
    #[serde(rename = "resize-size")]
    #[default(640u32)]
    pub resize_size: ConfigProperty<u32>,

    /// Height of each card's preview image in pixels.
    #[serde(rename = "widget-size")]
    #[default(150u32)]
    pub widget_size: ConfigProperty<u32>,

    /// Region selection command, parsed with shell-word splitting.
    #[serde(rename = "region-command")]
    #[default(String::from("slurp -f '%o@%x,%y,%w,%h'"))]
    pub region_command: ConfigProperty<String>,

    /// Spacing between window cards in pixels.
    #[serde(rename = "windows-spacing")]
    #[default(12u32)]
    pub windows_spacing: ConfigProperty<u32>,

    /// Minimum window cards per row.
    #[serde(rename = "windows-min-per-row")]
    #[default(3u32)]
    pub windows_min_per_row: ConfigProperty<u32>,

    /// Maximum window cards per row.
    #[serde(rename = "windows-max-per-row")]
    #[default(4u32)]
    pub windows_max_per_row: ConfigProperty<u32>,

    /// Spacing between output cards in pixels (applied per side).
    #[serde(rename = "outputs-spacing")]
    #[default(6u32)]
    pub outputs_spacing: ConfigProperty<u32>,

    /// Show the output name label under each output card.
    #[serde(rename = "outputs-show-label")]
    #[default(false)]
    pub outputs_show_label: ConfigProperty<bool>,

    /// Scale output cards by their fractional scale.
    #[serde(rename = "outputs-respect-scaling")]
    #[default(true)]
    pub outputs_respect_scaling: ConfigProperty<bool>,
}

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

crate::register_module!(SharePickerConfig);
