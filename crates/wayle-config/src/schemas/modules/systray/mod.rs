use schemars::{JsonSchema, schema_for};
use serde::{Deserialize, Serialize};
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::styling::{ColorValue, CssToken, Size},
};

/// System tray icons via the StatusNotifierItem protocol.
#[wayle_config(bar_container, i18n_prefix = "settings-modules-systray")]
pub struct SystrayConfig {
    /// Tray item icon size. Accepts a scale multiplier or pixels (e.g. `"20px"`).
    #[serde(rename = "icon-scale")]
    #[default(Size::Scale(1.0))]
    pub icon_scale: ConfigProperty<Size>,

    /// Gap between tray items. Accepts a scale multiplier or pixels (e.g. `"4px"`).
    #[serde(rename = "item-gap")]
    #[default(Size::Scale(0.25))]
    pub item_gap: ConfigProperty<Size>,

    /// Padding at the ends of the container. Accepts a scale multiplier or pixels (e.g. `"8px"`).
    ///
    /// Applies to left/right edges for horizontal bars, or top/bottom edges
    /// for vertical bars.
    #[serde(rename = "internal-padding")]
    #[default(Size::Scale(0.5))]
    pub internal_padding: ConfigProperty<Size>,

    /// Glob patterns for tray items to hide.
    ///
    /// Matches against item ID or title.
    /// Example: `["*discord*", "Steam"]`
    #[default(Vec::new())]
    pub blacklist: ConfigProperty<Vec<String>>,

    /// Custom icon and color overrides.
    ///
    /// First matching override wins. Supports glob patterns.
    ///
    /// ```toml
    /// [[module.systray.overrides]]
    /// name = "*discord*"
    /// icon = "si-discord-symbolic"
    /// color = "blue"
    /// ```
    #[default(Vec::new())]
    pub overrides: ConfigProperty<Vec<TrayItemOverride>>,

    /// Display border around container.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color token.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::BorderAccent))]
    pub border_color: ConfigProperty<ColorValue>,

    /// Container background color token.
    #[serde(rename = "button-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub button_bg_color: ConfigProperty<ColorValue>,
}

impl ModuleInfoProvider for SystrayConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("systray"),
            schema: || schema_for!(SystrayConfig),
            layout_id: Some(String::from("systray")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

/// Custom icon and color override for tray items matching a pattern.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, JsonSchema)]
pub struct TrayItemOverride {
    /// Glob pattern to match against item ID or title.
    ///
    /// Examples: `"discord"`, `"*Discord*"`, `"org.kde.*"`
    pub name: String,
    /// Custom icon name (symbolic icon).
    pub icon: Option<String>,
    /// Custom icon color.
    pub color: Option<ColorValue>,
}

crate::register_module!(SystrayConfig);

/// Base size (in rem) the `icon_scale` multiplier resolves against
/// (`Scale(1.0)` = default). Shared by the shell resolver and the settings
/// editor's scale↔px conversion.
pub const ICON_BASE_REM: f32 = 1.25;
