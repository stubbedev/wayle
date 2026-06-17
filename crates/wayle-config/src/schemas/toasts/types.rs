use schemars::{JsonSchema, schema_for};
use serde::{Deserialize, Serialize};

use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};

/// A reusable toast preset, triggerable by id with `wayle toast --preset <id>`.
///
/// A preset captures a toast's text, icon, optional progress bar, duration, and
/// CSS class so it can be fired by name. Any field can still be overridden per
/// invocation on the command line (or over the widget socket).
///
/// ## Example
///
/// ```toml
/// [[toasts.presets]]
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

    /// Auto-dismiss duration in milliseconds. Unset falls back to the toast
    /// config duration.
    #[serde(rename = "duration-ms", default)]
    pub duration_ms: Option<u32>,

    /// Extra CSS class applied to the toast for custom styling.
    #[serde(default)]
    pub class: Option<String>,
}

impl ModuleInfoProvider for ToastPreset {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("toast-preset"),
            schema: || schema_for!(ToastPreset),
            layout_id: None,
            array_entry: true,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

crate::register_module!(ToastPreset);
