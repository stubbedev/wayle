use schemars::schema_for;
use wayle_derive::wayle_config;

use crate::{
    ClickAction, ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::styling::{ColorValue, CssToken},
};

/// Shutdown, reboot, and logout menu.
#[wayle_config(i18n_prefix = "settings-modules-power")]
pub struct PowerConfig {
    /// Icon name to display.
    #[serde(rename = "icon-name")]
    #[default(String::from("ld-power-symbolic"))]
    pub icon_name: ConfigProperty<String>,

    /// Display border around button.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color token.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::Red))]
    pub border_color: ConfigProperty<ColorValue>,

    /// Icon foreground color. Auto selects based on variant for contrast.
    #[serde(rename = "icon-color")]
    #[default(ColorValue::Auto)]
    pub icon_color: ConfigProperty<ColorValue>,

    /// Icon container background color token.
    #[serde(rename = "icon-bg-color")]
    #[default(ColorValue::Token(CssToken::Red))]
    pub icon_bg_color: ConfigProperty<ColorValue>,

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

    /// Action on left click. Default opens wayle's native power menu (`:menu`).
    #[serde(rename = "left-click")]
    #[default(ClickAction::Shell(String::from(":menu")))]
    pub left_click: ConfigProperty<ClickAction>,

    /// Command run by the power menu's Lock button.
    #[serde(rename = "lock-command")]
    #[default(String::from("loginctl lock-session"))]
    pub lock_command: ConfigProperty<String>,

    /// Command run by the power menu's Log out button.
    #[serde(rename = "logout-command")]
    #[default(String::from("loginctl terminate-session $XDG_SESSION_ID"))]
    pub logout_command: ConfigProperty<String>,

    /// Command run by the power menu's Suspend button.
    #[serde(rename = "suspend-command")]
    #[default(String::from("systemctl suspend"))]
    pub suspend_command: ConfigProperty<String>,

    /// Command run by the power menu's Reboot button.
    #[serde(rename = "reboot-command")]
    #[default(String::from("systemctl reboot"))]
    pub reboot_command: ConfigProperty<String>,

    /// Command run by the power menu's Shut down button.
    #[serde(rename = "shutdown-command")]
    #[default(String::from("systemctl poweroff"))]
    pub shutdown_command: ConfigProperty<String>,

    /// Show the Lock button in the power menu.
    #[serde(rename = "show-lock")]
    #[default(true)]
    pub show_lock: ConfigProperty<bool>,

    /// Show the Log out button in the power menu.
    #[serde(rename = "show-logout")]
    #[default(true)]
    pub show_logout: ConfigProperty<bool>,

    /// Show the Suspend button in the power menu.
    #[serde(rename = "show-suspend")]
    #[default(true)]
    pub show_suspend: ConfigProperty<bool>,

    /// Show the Reboot button in the power menu.
    #[serde(rename = "show-reboot")]
    #[default(true)]
    pub show_reboot: ConfigProperty<bool>,

    /// Show the Shut down button in the power menu.
    #[serde(rename = "show-shutdown")]
    #[default(true)]
    pub show_shutdown: ConfigProperty<bool>,

    /// Hidden: icon always shown.
    #[serde(skip)]
    #[schemars(skip)]
    #[wayle(skip)]
    #[i18n(skip)]
    #[default(true)]
    pub icon_show: ConfigProperty<bool>,

    /// Hidden: label visibility (always false).
    #[serde(skip)]
    #[schemars(skip)]
    #[wayle(skip)]
    #[i18n(skip)]
    #[default(false)]
    pub label_show: ConfigProperty<bool>,

    /// Hidden: label color (unused).
    #[serde(skip)]
    #[schemars(skip)]
    #[wayle(skip)]
    #[i18n(skip)]
    #[default(ColorValue::Token(CssToken::Red))]
    pub label_color: ConfigProperty<ColorValue>,

    /// Hidden: label max length (unused).
    #[serde(skip)]
    #[schemars(skip)]
    #[wayle(skip)]
    #[i18n(skip)]
    #[default(0)]
    pub label_max_length: ConfigProperty<u32>,

    /// Hidden: button background (unused).
    #[serde(skip)]
    #[schemars(skip)]
    #[wayle(skip)]
    #[i18n(skip)]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub button_bg_color: ConfigProperty<ColorValue>,
}

impl ModuleInfoProvider for PowerConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("power"),
            schema: || schema_for!(PowerConfig),
            layout_id: Some(String::from("power")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::bar_button()
    }
}

crate::register_module!(PowerConfig);
