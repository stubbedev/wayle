#[cfg(feature = "schema")]
use schemars::schema_for;
use wayle_derive::wayle_config;

#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{
    ClickAction, ConfigProperty,
    schemas::styling::{ColorValue, CssToken},
};

/// Screenshot capture button.
///
/// Click the bar button to capture a region, output, or window. Controllable
/// from the CLI / RPC socket: `wayle screenshot region|output|window`.
#[wayle_config(bar_button, i18n_prefix = "settings-modules-screenshot")]
pub struct ScreenshotConfig {
    /// Bar button icon.
    #[serde(rename = "icon")]
    #[default(String::from("ld-camera-symbolic"))]
    pub icon: ConfigProperty<String>,

    /// Output directory for screenshots. Empty uses the XDG Pictures directory.
    #[serde(rename = "output-directory")]
    #[default(String::new())]
    pub output_directory: ConfigProperty<String>,

    /// Saved file name, formatted with `chrono`/`strftime` specifiers.
    #[serde(rename = "filename-format")]
    #[default(String::from("Screenshot_%Y-%m-%d_%H-%M-%S.png"))]
    pub filename_format: ConfigProperty<String>,

    /// Copy the captured image to the clipboard.
    #[serde(rename = "copy-to-clipboard")]
    #[default(true)]
    pub copy_to_clipboard: ConfigProperty<bool>,

    /// Show a desktop notification after capturing.
    #[serde(rename = "notify")]
    #[default(true)]
    pub notify: ConfigProperty<bool>,

    /// Display border around button.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color token.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::Accent))]
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
    #[default(ColorValue::Token(CssToken::Accent))]
    pub icon_bg_color: ConfigProperty<ColorValue>,

    /// Static label text shown beside the icon.
    #[serde(rename = "label")]
    #[default(String::new())]
    pub label: ConfigProperty<String>,

    /// Display label.
    #[serde(rename = "label-show")]
    #[default(false)]
    pub label_show: ConfigProperty<bool>,

    /// Label text color token.
    #[serde(rename = "label-color")]
    #[default(ColorValue::Token(CssToken::Accent))]
    pub label_color: ConfigProperty<ColorValue>,

    /// Max label characters before truncation with ellipsis. Set to 0 to disable.
    #[serde(rename = "label-max-length")]
    #[default(0)]
    pub label_max_length: ConfigProperty<u32>,

    /// Button background color token.
    #[serde(rename = "button-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub button_bg_color: ConfigProperty<ColorValue>,

    /// Action on left click. Default captures a region.
    #[serde(rename = "left-click")]
    #[default(ClickAction::Shell(String::from("wayle screenshot region")))]
    pub left_click: ConfigProperty<ClickAction>,

    /// Action on right click. Default captures the focused output.
    #[serde(rename = "right-click")]
    #[default(ClickAction::Shell(String::from("wayle screenshot output")))]
    pub right_click: ConfigProperty<ClickAction>,

    /// Action on middle click. Default captures the active window.
    #[serde(rename = "middle-click")]
    #[default(ClickAction::Shell(String::from("wayle screenshot window")))]
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

#[cfg(feature = "schema")]
impl ModuleInfoProvider for ScreenshotConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("screenshot"),
            schema: || schema_for!(ScreenshotConfig),
            layout_id: Some(String::from("screenshot")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::bar_button()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(ScreenshotConfig);
