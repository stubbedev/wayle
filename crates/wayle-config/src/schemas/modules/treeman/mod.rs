#[cfg(feature = "schema")]
use schemars::schema_for;
use wayle_derive::wayle_config;

#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{
    ClickAction, ConfigProperty,
    schemas::styling::{ColorValue, CssToken},
};

/// treeman worktree health across all registered repos, in a dropdown.
#[wayle_config(bar_button, i18n_prefix = "settings-modules-treeman")]
pub struct TreemanConfig {
    /// Format string for the label.
    ///
    /// ## Placeholders
    ///
    /// - `{{ total }}` - Total active worktrees
    /// - `{{ stable }}` - Worktrees in the ready bucket
    /// - `{{ up }}` - Worktrees preparing
    /// - `{{ down }}` - Worktrees tearing down
    /// - `{{ failed }}` - Worktrees whose last finalize errored
    ///
    /// ## Examples
    ///
    /// - `"{{ total }}"` - "7"
    /// - `"{{ total }} ({{ failed }}!)"` - "7 (1!)"
    #[serde(rename = "format")]
    #[default(String::from("{{ total }}"))]
    pub format: ConfigProperty<String>,

    /// Module icon (shown when no worktree has failed).
    #[serde(rename = "icon-name")]
    #[default(String::from("ld-layers-symbolic"))]
    pub icon_name: ConfigProperty<String>,

    /// Icon shown when any worktree's last finalize errored.
    #[serde(rename = "icon-failed")]
    #[default(String::from("tb-alert-triangle-symbolic"))]
    pub icon_failed: ConfigProperty<String>,

    /// Collapse the module entirely when there are no active worktrees.
    #[serde(rename = "hide-if-empty")]
    #[default(false)]
    pub hide_if_empty: ConfigProperty<bool>,

    /// Display border around button.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color token.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::BorderAccent))]
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

    /// Display count label.
    #[serde(rename = "label-show")]
    #[default(true)]
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

    /// Action on left click.
    #[serde(rename = "left-click")]
    #[default(ClickAction::Dropdown(String::from("treeman")))]
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

#[cfg(feature = "schema")]
impl ModuleInfoProvider for TreemanConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("treeman"),
            schema: || schema_for!(TreemanConfig),
            layout_id: Some(String::from("treeman")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::bar_button()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(TreemanConfig);
