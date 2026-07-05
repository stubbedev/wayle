//! MangoWM tag switcher configuration.
//!
//! Mango is dwm-derived: each monitor has a fixed set of numbered tags rather
//! than a growable workspace list. The config mirrors the niri/Hyprland
//! workspace switchers, minus the name-based label strategy. Tags have no
//! native name, so a tag's label is its index unless a `tag-map` entry
//! overrides it.

use std::collections::HashMap;

#[cfg(feature = "schema")]
use schemars::schema_for;
use wayle_derive::wayle_config;

use super::{ActiveIndicator, DisplayMode, UrgentMode, WorkspaceClickAction, WorkspaceStyle};
#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{
    ConfigProperty,
    schemas::styling::{ColorValue, CssToken, Size},
};

/// MangoWM tag switcher module configuration.
#[wayle_config(i18n_prefix = "settings-modules-mango-workspaces")]
pub struct MangoWorkspacesConfig {
    /// Hide tags that hold no clients and are not active.
    #[serde(rename = "hide-empty")]
    #[default(true)]
    pub hide_empty: ConfigProperty<bool>,

    /// Always show tags up to this one-based index, even when empty.
    ///
    /// `0` shows only occupied or active tags (subject to `hide-empty`). A
    /// value above the compositor's tag count just shows every tag.
    #[serde(rename = "min-tag-count")]
    #[default(0)]
    pub min_tag_count: ConfigProperty<u8>,

    /// What identifies each tag: its label, an icon, or nothing.
    #[serde(rename = "display-mode")]
    #[default(DisplayMode::Label)]
    pub display_mode: ConfigProperty<DisplayMode>,

    /// Text shown between the tag label and its application icons.
    #[serde(rename = "divider")]
    #[default(String::from(" "))]
    pub divider: ConfigProperty<String>,

    /// Show an application icon per client on each tag.
    #[serde(rename = "app-icons-show")]
    #[default(false)]
    pub app_icons_show: ConfigProperty<bool>,

    /// Collapse clients that share an application to a single icon.
    #[serde(rename = "app-icons-dedupe")]
    #[default(true)]
    pub app_icons_dedupe: ConfigProperty<bool>,

    /// Icon for clients not matched by `app-icon-map`.
    #[serde(rename = "app-icons-fallback")]
    #[default(String::from("ld-app-window-symbolic"))]
    pub app_icons_fallback: ConfigProperty<String>,

    /// Icon shown when a tag has no clients.
    #[serde(rename = "app-icons-empty")]
    #[default(String::from("tb-minus-symbolic"))]
    pub app_icons_empty: ConfigProperty<String>,

    /// Highlight tags whose clients requested attention.
    #[serde(rename = "urgent-show")]
    #[default(true)]
    pub urgent_show: ConfigProperty<bool>,

    /// Whether urgency is tracked per tag or per application.
    #[serde(rename = "urgent-mode")]
    #[default(UrgentMode::Workspace)]
    pub urgent_mode: ConfigProperty<UrgentMode>,

    /// How the active tag is marked.
    #[serde(rename = "active-indicator")]
    #[default(ActiveIndicator::Background)]
    pub active_indicator: ConfigProperty<ActiveIndicator>,

    /// Padding around each tag button, in rem.
    #[serde(rename = "tag-padding")]
    #[default(Size::Scale(0.5))]
    pub tag_padding: ConfigProperty<Size>,

    /// Spacing between application icons. Accepts a scale multiplier or pixels (e.g. `"4px"`).
    #[serde(rename = "icon-gap")]
    #[default(Size::Scale(0.3))]
    pub icon_gap: ConfigProperty<Size>,

    /// Application icon size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
    #[serde(rename = "icon-size")]
    #[default(Size::default())]
    pub icon_size: ConfigProperty<Size>,

    /// Tag label text size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
    #[serde(rename = "label-size")]
    #[default(Size::default())]
    pub label_size: ConfigProperty<Size>,

    /// Color of the active tag.
    #[serde(rename = "active-color")]
    #[default(ColorValue::Token(CssToken::Accent))]
    pub active_color: ConfigProperty<ColorValue>,

    /// Color of tags that hold clients but are not active.
    #[serde(rename = "occupied-color")]
    #[default(ColorValue::Token(CssToken::FgMuted))]
    pub occupied_color: ConfigProperty<ColorValue>,

    /// Color of empty tags.
    #[serde(rename = "empty-color")]
    #[default(ColorValue::Token(CssToken::FgSubtle))]
    pub empty_color: ConfigProperty<ColorValue>,

    /// Background color of the tag container.
    #[serde(rename = "container-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub container_bg_color: ConfigProperty<ColorValue>,

    /// Draw a border around the tag container.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color when the border is shown.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::BorderDefault))]
    pub border_color: ConfigProperty<ColorValue>,

    /// Window-to-icon mappings for the application icons.
    ///
    /// Keys are glob patterns matched against a client's app id, or `title:`
    /// patterns matched against its title. Values are symbolic icon names.
    ///
    /// ## Example
    ///
    /// ```toml
    /// [modules.mango-workspaces.app-icon-map]
    /// "*firefox*" = "ld-globe-symbolic"
    /// "title:*YouTube*" = "si-youtube-symbolic"
    /// ```
    #[serde(rename = "app-icon-map")]
    #[default(HashMap::new())]
    pub app_icon_map: ConfigProperty<HashMap<String, String>>,

    /// Per-tag icon and color overrides, keyed by one-based tag index.
    ///
    /// ## Example
    ///
    /// ```toml
    /// [modules.mango-workspaces.tag-map.1]
    /// label = "web"
    /// icon = "ld-globe-symbolic"
    /// color = "#4a90d9"
    ///
    /// [modules.mango-workspaces.tag-map.2]
    /// label = "term"
    /// icon = "ld-terminal-symbolic"
    /// ```
    #[serde(rename = "tag-map")]
    #[default(HashMap::new())]
    pub tag_map: ConfigProperty<HashMap<String, WorkspaceStyle>>,

    /// Action for a left click on a tag.
    #[serde(rename = "left-click")]
    #[default(WorkspaceClickAction::FocusWorkspace)]
    pub left_click: ConfigProperty<WorkspaceClickAction>,

    /// Action for a middle click on a tag.
    #[serde(rename = "middle-click")]
    #[default(WorkspaceClickAction::None)]
    pub middle_click: ConfigProperty<WorkspaceClickAction>,

    /// Action for a right click on a tag.
    #[serde(rename = "right-click")]
    #[default(WorkspaceClickAction::None)]
    pub right_click: ConfigProperty<WorkspaceClickAction>,

    /// Action for scrolling up over the tag container.
    #[serde(rename = "scroll-up")]
    #[default(WorkspaceClickAction::FocusPrevious)]
    pub scroll_up: ConfigProperty<WorkspaceClickAction>,

    /// Action for scrolling down over the tag container.
    #[serde(rename = "scroll-down")]
    #[default(WorkspaceClickAction::FocusNext)]
    pub scroll_down: ConfigProperty<WorkspaceClickAction>,
}

#[cfg(feature = "schema")]
impl ModuleInfoProvider for MangoWorkspacesConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("mango-workspaces"),
            schema: || schema_for!(MangoWorkspacesConfig),
            layout_id: Some(String::from("mango-workspaces")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(MangoWorkspacesConfig);

/// Base size (in rem) the `icon_size` scale multiplier resolves against
/// (`Scale(1.0)` = default). Shared by the shell resolver and the settings
/// editor's scale↔px conversion.
pub const ICON_BASE_REM: f32 = 1.3;
/// Base size (in rem) the `label_size` scale multiplier resolves against.
pub const LABEL_BASE_REM: f32 = 1.1;
