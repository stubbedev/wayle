use std::{borrow::Cow, fmt};

use schemars::JsonSchema;
use serde::{Deserialize, Serialize, Serializer, de};
use wayle_derive::wayle_enum;

/// Screen anchor for the OSD overlay.
#[wayle_enum(default)]
pub enum OsdPosition {
    /// Top-left corner.
    TopLeft,
    /// Top-center edge.
    Top,
    /// Top-right corner.
    TopRight,
    /// Right-center edge.
    Right,
    /// Bottom-right corner.
    BottomRight,
    /// Bottom-center edge.
    #[default]
    Bottom,
    /// Bottom-left corner.
    BottomLeft,
    /// Left-center edge.
    Left,
}

/// Horizontal alignment of OSD toast/toggle content.
#[wayle_enum(default)]
pub enum OsdTextAlign {
    /// Align content to the start (left in LTR layouts).
    Start,
    /// Center content horizontally.
    #[default]
    Center,
    /// Align content to the end (right in LTR layouts).
    End,
}

/// Target monitor for the OSD overlay.
///
/// Accepts `"primary"` or a connector name like `"DP-1"`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub enum OsdMonitor {
    /// Use the first available monitor.
    #[default]
    Primary,
    /// Use a specific monitor identified by connector name.
    Connector(String),
}

impl Serialize for OsdMonitor {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        match self {
            Self::Primary => serializer.serialize_str("primary"),
            Self::Connector(name) => serializer.serialize_str(name),
        }
    }
}

impl<'de> Deserialize<'de> for OsdMonitor {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: de::Deserializer<'de>,
    {
        deserializer.deserialize_str(OsdMonitorVisitor)
    }
}

impl JsonSchema for OsdMonitor {
    fn schema_name() -> Cow<'static, str> {
        Cow::Borrowed("OsdMonitor")
    }

    fn json_schema(_generator: &mut schemars::SchemaGenerator) -> schemars::Schema {
        schemars::json_schema!({
            "type": "string",
            "description": "\"primary\" or a monitor connector name (e.g. \"DP-1\")",
            "default": "primary"
        })
    }
}

struct OsdMonitorVisitor;

impl de::Visitor<'_> for OsdMonitorVisitor {
    type Value = OsdMonitor;

    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str(r#""primary" or a connector name like "DP-1""#)
    }

    fn visit_str<E: de::Error>(self, value: &str) -> Result<OsdMonitor, E> {
        if value.eq_ignore_ascii_case("primary") {
            Ok(OsdMonitor::Primary)
        } else {
            Ok(OsdMonitor::Connector(value.to_owned()))
        }
    }
}

/// A reusable toast preset, triggerable by id with `wayle toast --preset <id>`.
///
/// A preset captures a toast's text, icon, optional progress bar, duration, and
/// CSS class so it can be fired by name. Any field can still be overridden per
/// invocation on the command line (or over the widget socket).
///
/// ## Example
///
/// ```toml
/// [[osd.presets]]
/// id = "saved"
/// label = "Saved"
/// icon = "ld-check-symbolic"
/// duration-ms = 1500
/// class = "success"
///
/// # Fire it: wayle toast --preset saved
/// ```
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "kebab-case")]
pub struct ToastPreset {
    /// Unique identifier. Trigger with `wayle toast --preset <id>`.
    pub id: String,

    /// Toast text. An explicit label on the command line overrides this.
    #[serde(default)]
    pub label: Option<String>,

    /// Symbolic icon name shown beside the text.
    #[serde(default)]
    pub icon: Option<String>,

    /// Progress percentage (0-100). When set, renders a progress bar instead
    /// of a plain icon + label toast.
    #[serde(default)]
    pub percentage: Option<f64>,

    /// Auto-dismiss duration in milliseconds. Unset falls back to the OSD
    /// config duration.
    #[serde(rename = "duration-ms", default)]
    pub duration_ms: Option<u32>,

    /// Extra CSS class applied to the toast for custom styling.
    #[serde(default)]
    pub class: Option<String>,
}

impl crate::docs::ModuleInfoProvider for ToastPreset {
    fn module_info() -> crate::docs::ModuleInfo {
        crate::docs::ModuleInfo {
            name: String::from("toast-preset"),
            schema: || schemars::schema_for!(ToastPreset),
            layout_id: None,
            array_entry: true,
        }
    }

    fn groups() -> Vec<crate::docs::ConfigGroup> {
        crate::docs::GroupDefaults::standard()
    }
}

crate::register_module!(ToastPreset);
