use schemars::schema_for;
use wayle_derive::wayle_config;

use crate::{
    ClickAction, ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::styling::{ColorValue, CssToken},
};

/// Power profile indicator and switcher (power-profiles-daemon).
///
/// Shows the active profile with a per-profile icon and color, and cycles
/// through the available profiles on click. Backed by the same
/// power-profiles-daemon D-Bus interface as `powerprofilesctl`.
#[wayle_config(bar_button, i18n_prefix = "settings-modules-power-profiles")]
pub struct PowerProfilesConfig {
    /// Format string for the label.
    ///
    /// ## Placeholders
    ///
    /// - `{{ profile }}` - Active profile name (power-saver, balanced, performance)
    ///
    /// ## Examples
    ///
    /// - `"{{ profile }}"` - "balanced"
    #[serde(rename = "format")]
    #[default(String::from("{{ profile }}"))]
    pub format: ConfigProperty<String>,

    /// Icon shown while the power-saver profile is active.
    #[serde(rename = "icon-power-saver")]
    #[default(String::from("ld-leaf-symbolic"))]
    pub icon_power_saver: ConfigProperty<String>,

    /// Icon shown while the balanced profile is active.
    #[serde(rename = "icon-balanced")]
    #[default(String::from("ld-scale-symbolic"))]
    pub icon_balanced: ConfigProperty<String>,

    /// Icon shown while the performance profile is active.
    #[serde(rename = "icon-performance")]
    #[default(String::from("ld-rocket-symbolic"))]
    pub icon_performance: ConfigProperty<String>,

    /// Icon/label color while the power-saver profile is active.
    #[serde(rename = "color-power-saver")]
    #[default(ColorValue::Token(CssToken::Green))]
    pub color_power_saver: ConfigProperty<ColorValue>,

    /// Icon/label color while the balanced profile is active.
    #[serde(rename = "color-balanced")]
    #[default(ColorValue::Token(CssToken::Blue))]
    pub color_balanced: ConfigProperty<ColorValue>,

    /// Icon/label color while the performance profile is active.
    #[serde(rename = "color-performance")]
    #[default(ColorValue::Token(CssToken::Red))]
    pub color_performance: ConfigProperty<ColorValue>,

    /// Display border around button.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color token.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::Blue))]
    pub border_color: ConfigProperty<ColorValue>,

    /// Display module icon.
    #[serde(rename = "icon-show")]
    #[default(true)]
    pub icon_show: ConfigProperty<bool>,

    /// Icon foreground color. Auto selects based on variant for contrast.
    ///
    /// Overridden per active profile by the `color-*` fields.
    #[serde(rename = "icon-color")]
    #[default(ColorValue::Auto)]
    pub icon_color: ConfigProperty<ColorValue>,

    /// Icon container background color token.
    #[serde(rename = "icon-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub icon_bg_color: ConfigProperty<ColorValue>,

    /// Display label.
    #[serde(rename = "label-show")]
    #[default(false)]
    pub label_show: ConfigProperty<bool>,

    /// Label text color token.
    #[serde(rename = "label-color")]
    #[default(ColorValue::Auto)]
    pub label_color: ConfigProperty<ColorValue>,

    /// Max label characters before truncation with ellipsis. Set to 0 to disable.
    #[serde(rename = "label-max-length")]
    #[default(0)]
    pub label_max_length: ConfigProperty<u32>,

    /// Button background color token.
    #[serde(rename = "button-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub button_bg_color: ConfigProperty<ColorValue>,

    /// Action on left click. Default cycles to the next power profile.
    #[serde(rename = "left-click")]
    #[default(ClickAction::Shell(String::from(":cycle")))]
    pub left_click: ConfigProperty<ClickAction>,

    /// Action on right click.
    #[serde(rename = "right-click")]
    #[default(ClickAction::None)]
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

impl ModuleInfoProvider for PowerProfilesConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("power-profiles"),
            schema: || schema_for!(PowerProfilesConfig),
            layout_id: Some(String::from("power-profiles")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::bar_button()
    }
}

crate::register_module!(PowerProfilesConfig);
