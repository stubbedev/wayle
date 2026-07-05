use serde::{Deserialize, Serialize};

use crate::schemas::styling::Size;

/// Size override for a dropdown foldout panel.
///
/// Each field is optional: when unset the dropdown keeps its built-in default
/// size (scaled by the global scale). A [`Size`] may be a scale multiplier of
/// the built-in base (e.g. `1.5`) or an absolute pixel length (e.g. `"480px"`).
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[derive(Debug, Clone, Copy, PartialEq, Default, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case", default)]
pub struct DropdownSize {
    /// Panel width override. Unset uses the built-in default width.
    pub width: Option<Size>,

    /// Panel height override. Unset uses the built-in default height. Has no
    /// effect on dropdowns whose height grows to fit their content.
    pub height: Option<Size>,
}
