use schemars::schema_for;
use wayle_derive::wayle_config;

use crate::{
    ClickAction, ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::styling::{ColorValue, CssToken},
};

/// Unread mail count, backed by a notmuch query.
///
/// Runs `notmuch count <query>` and re-queries whenever the maildir changes
/// (event-driven via an inotify watch on the notmuch database path). Hidden
/// while the count is zero when `hide-when-zero` is set.
#[wayle_config(bar_button, i18n_prefix = "settings-modules-mail")]
pub struct MailConfig {
    /// Format string for the label.
    ///
    /// ## Placeholders
    ///
    /// - `{{ count }}` - Number of messages matching the query
    ///
    /// ## Examples
    ///
    /// - `"{{ count }}"` - "3"
    #[serde(rename = "format")]
    #[default(String::from("{{ count }}"))]
    pub format: ConfigProperty<String>,

    /// notmuch search query whose match count is shown.
    ///
    /// Any query `notmuch count` accepts, e.g. `tag:unread`,
    /// `tag:unread and tag:inbox`, `folder:work and tag:unread`.
    #[serde(rename = "query")]
    #[default(String::from("tag:unread"))]
    pub query: ConfigProperty<String>,

    /// Hide the module entirely while the count is zero.
    #[serde(rename = "hide-when-zero")]
    #[default(true)]
    pub hide_when_zero: ConfigProperty<bool>,

    /// Module icon.
    #[serde(rename = "icon-name")]
    #[default(String::from("ld-mail-symbolic"))]
    pub icon_name: ConfigProperty<String>,

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
    #[serde(rename = "icon-color")]
    #[default(ColorValue::Auto)]
    pub icon_color: ConfigProperty<ColorValue>,

    /// Icon container background color token.
    #[serde(rename = "icon-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub icon_bg_color: ConfigProperty<ColorValue>,

    /// Display label.
    #[serde(rename = "label-show")]
    #[default(true)]
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

    /// Action on left click. Empty for no action, or a shell command (e.g. your mail client).
    #[serde(rename = "left-click")]
    #[default(ClickAction::None)]
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

impl ModuleInfoProvider for MailConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("mail"),
            schema: || schema_for!(MailConfig),
            layout_id: Some(String::from("mail")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::bar_button()
    }
}

crate::register_module!(MailConfig);
