//! sway workspace switcher configuration.
//!
//! sway uses a growable, dynamically-numbered workspace model much like niri,
//! so this module reuses the shared [`LabelStrategy`], [`WorkspaceClickAction`],
//! and [`WorkspaceMap`] types defined alongside the niri configuration.

use std::collections::BTreeMap;

#[cfg(feature = "schema")]
use schemars::schema_for;
use wayle_derive::wayle_config;

use super::{
    ActiveIndicator, DisplayMode, UrgentMode,
    niri_workspaces::{LabelStrategy, WorkspaceClickAction, WorkspaceMap},
};
#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{
    ConfigProperty,
    schemas::styling::{ColorValue, CssToken, Size},
};

/// sway workspace indicators with click-to-switch.
#[wayle_config(i18n_prefix = "settings-modules-sway-workspaces")]
pub struct SwayWorkspacesConfig {
    /// Show only workspaces on this bar's monitor.
    ///
    /// When `true` (default), each bar shows only its own output's
    /// workspaces. When `false`, all workspaces from every output are shown.
    #[serde(rename = "monitor-specific")]
    #[default(true)]
    pub monitor_specific: ConfigProperty<bool>,

    /// Hide the trailing empty workspace on each output.
    ///
    /// Off by default, since sway does not auto-allocate a trailing empty
    /// workspace the way niri does. Enable it if your config keeps one around.
    #[serde(rename = "hide-trailing-empty")]
    #[default(false)]
    pub hide_trailing_empty: ConfigProperty<bool>,

    /// What identifies each workspace button.
    ///
    /// - `label` (default): show the workspace label per `label-strategy`
    /// - `icon`: show an icon from `workspace-map` (falls back to label if unmapped)
    /// - `none`: show nothing — only app icons visible (if enabled)
    #[serde(rename = "display-mode")]
    #[default(DisplayMode::Label)]
    pub display_mode: ConfigProperty<DisplayMode>,

    /// How to compose the workspace label when `display-mode = "label"`.
    ///
    /// - `index`: number only (`"1"`, `"2"`)
    /// - `name-or-index` (default): name when set, number otherwise
    /// - `name-only`: name only; unnamed workspaces show nothing
    /// - `index-and-name`: `"1: web"` form; unnamed workspaces show the number alone
    #[serde(rename = "label-strategy")]
    #[default(LabelStrategy::NameOrIndex)]
    pub label_strategy: ConfigProperty<LabelStrategy>,

    /// Pulse animation on workspaces with urgent windows.
    #[serde(rename = "urgent-show")]
    #[default(true)]
    pub urgent_show: ConfigProperty<bool>,

    /// Where the urgent pulse is applied.
    ///
    /// - `workspace` (default): whole button pulses
    /// - `application`: only the urgent app icon pulses, falling back to
    ///   `workspace` when app icons are disabled
    #[serde(rename = "urgent-mode")]
    #[default(UrgentMode::Workspace)]
    pub urgent_mode: ConfigProperty<UrgentMode>,

    /// Visual indicator for the active workspace.
    #[serde(rename = "active-indicator")]
    #[default(ActiveIndicator::Background)]
    pub active_indicator: ConfigProperty<ActiveIndicator>,

    /// Text separator between workspace identity and app icons.
    #[serde(rename = "divider")]
    #[default(String::from(" "))]
    pub divider: ConfigProperty<String>,

    /// Show application icons for windows on each workspace.
    #[serde(rename = "app-icons-show")]
    #[default(false)]
    pub app_icons_show: ConfigProperty<bool>,

    /// Deduplicate application icons within a workspace.
    ///
    /// When `true`, one icon per unique `app_id`. When `false`, one icon
    /// per window.
    #[serde(rename = "app-icons-dedupe")]
    #[default(true)]
    pub app_icons_dedupe: ConfigProperty<bool>,

    /// Fallback icon for applications not matched by `app-icon-map`.
    #[serde(rename = "app-icons-fallback")]
    #[default(String::from("ld-app-window-symbolic"))]
    pub app_icons_fallback: ConfigProperty<String>,

    /// Icon shown when a workspace has no application windows.
    #[serde(rename = "app-icons-empty")]
    #[default(String::from("tb-minus-symbolic"))]
    pub app_icons_empty: ConfigProperty<String>,

    /// Gap between app icons within a workspace button. Accepts a scale multiplier or pixels (e.g. `"4px"`).
    #[serde(rename = "icon-gap")]
    #[default(Size::Scale(0.3))]
    pub icon_gap: ConfigProperty<Size>,

    /// Padding for workspace content along the bar direction. Accepts a scale multiplier or pixels (e.g. `"8px"`).
    #[serde(rename = "workspace-padding")]
    #[default(Size::Scale(0.5))]
    pub workspace_padding: ConfigProperty<Size>,

    /// Workspace icon size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
    ///
    /// Applies to identity icons and custom icons from `workspace-map`.
    #[serde(rename = "icon-size")]
    #[default(Size::default())]
    pub icon_size: ConfigProperty<Size>,

    /// Workspace label and divider size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
    #[serde(rename = "label-size")]
    #[default(Size::default())]
    pub label_size: ConfigProperty<Size>,

    /// Workspaces to hide from the display.
    ///
    /// Glob patterns matched against the workspace's name, then its number,
    /// then its stable id. Examples:
    /// - `"scratch"` — hide the workspace named `scratch`
    /// - `"1?"` — hide numbers 10-19
    #[serde(rename = "workspace-ignore")]
    #[default(Vec::new())]
    pub workspace_ignore: ConfigProperty<Vec<String>>,

    /// Color for the active (visible on its output) workspace.
    ///
    /// In `background` indicator mode, also used as the button background.
    #[serde(rename = "active-color")]
    #[default(ColorValue::Token(CssToken::Accent))]
    pub active_color: ConfigProperty<ColorValue>,

    /// Color for occupied workspaces (have windows but not active).
    #[serde(rename = "occupied-color")]
    #[default(ColorValue::Token(CssToken::FgMuted))]
    pub occupied_color: ConfigProperty<ColorValue>,

    /// Color for empty workspaces and placeholder slots.
    #[serde(rename = "empty-color")]
    #[default(ColorValue::Token(CssToken::FgSubtle))]
    pub empty_color: ConfigProperty<ColorValue>,

    /// Background color for the workspaces container.
    #[serde(rename = "container-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub container_bg_color: ConfigProperty<ColorValue>,

    /// Display border around the workspaces container.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color for the workspaces container.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::BorderDefault))]
    pub border_color: ConfigProperty<ColorValue>,

    /// Per-workspace icon and color overrides, keyed by name or id-as-string.
    ///
    /// ## Example
    ///
    /// ```toml
    /// [modules.sway-workspaces.workspace-map]
    /// web = { icon = "ld-globe-symbolic", color = "#4a90d9" }
    /// terminal = { icon = "ld-terminal-symbolic" }
    /// ```
    #[serde(rename = "workspace-map")]
    #[default(WorkspaceMap::default())]
    pub workspace_map: ConfigProperty<WorkspaceMap>,

    /// Application icon mapping with glob pattern support.
    ///
    /// Maps window `app_id` or title to symbolic icon names. Supports:
    /// - No prefix: matches `app_id` (e.g. `"*firefox*"`)
    /// - `app:` prefix: explicit `app_id` match (e.g. `"app:org.mozilla.*"`)
    /// - `title:` prefix: matches window title (e.g. `"title:*YouTube*"`)
    ///
    /// ## Example
    ///
    /// ```toml
    /// [modules.sway-workspaces.app-icon-map]
    /// "*firefox*" = "ld-globe-symbolic"
    /// "title:*YouTube*" = "ld-youtube-symbolic"
    /// ```
    #[serde(rename = "app-icon-map")]
    #[default(BTreeMap::new())]
    pub app_icon_map: ConfigProperty<BTreeMap<String, String>>,

    /// Action on left click.
    #[serde(rename = "left-click")]
    #[default(WorkspaceClickAction::FocusWorkspace)]
    pub left_click: ConfigProperty<WorkspaceClickAction>,

    /// Action on middle click.
    #[serde(rename = "middle-click")]
    #[default(WorkspaceClickAction::None)]
    pub middle_click: ConfigProperty<WorkspaceClickAction>,

    /// Action on right click.
    #[serde(rename = "right-click")]
    #[default(WorkspaceClickAction::None)]
    pub right_click: ConfigProperty<WorkspaceClickAction>,

    /// Action on scroll up.
    #[serde(rename = "scroll-up")]
    #[default(WorkspaceClickAction::FocusPrevious)]
    pub scroll_up: ConfigProperty<WorkspaceClickAction>,

    /// Action on scroll down.
    #[serde(rename = "scroll-down")]
    #[default(WorkspaceClickAction::FocusNext)]
    pub scroll_down: ConfigProperty<WorkspaceClickAction>,
}

#[cfg(feature = "schema")]
impl ModuleInfoProvider for SwayWorkspacesConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("sway-workspaces"),
            schema: || schema_for!(SwayWorkspacesConfig),
            layout_id: Some(String::from("sway-workspaces")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(SwayWorkspacesConfig);

/// Base size (in rem) the `icon_size` scale multiplier resolves against
/// (`Scale(1.0)` = default). Shared by the shell resolver and the settings
/// editor's scale↔px conversion.
pub const ICON_BASE_REM: f32 = 1.3;
/// Base size (in rem) the `label_size` scale multiplier resolves against.
pub const LABEL_BASE_REM: f32 = 1.1;
