//! Size value: a scale multiplier or an absolute pixel length.
//!
//! Wherever a size is configured, the value may be given either as a bare
//! number (a scale multiplier applied on top of the element's base size and
//! the global/bar scale) or as a pixel string like `"24px"` (an absolute
//! length that ignores the scale multipliers entirely).

#[cfg(feature = "schema")]
use std::borrow::Cow;
use std::fmt::{self, Display, Formatter};

use serde::{Deserialize, Deserializer, Serialize, Serializer};
use tracing::warn;

/// Largest accepted pixel value, guarding against absurd configs.
const PX_MAX: f32 = 10_000.0;

/// Pixels per rem. A `Size::Scale` multiplier is a multiple of a field's base
/// size given in rem, so resolving one to pixels goes through this factor.
pub const REM_BASE_PX: f32 = 16.0;

/// A configurable size, expressed as either a scale multiplier or pixels.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Size {
    /// Multiplier of the element's base size, further multiplied by the
    /// global/bar scale. `1.0` means the default size.
    Scale(f32),

    /// Absolute pixel length, unaffected by the global/bar scale.
    Px(f32),
}

impl Size {
    /// Creates a scale multiplier, clamping negatives to `0.0`.
    #[must_use]
    pub fn scale(value: f32) -> Self {
        Self::Scale(value.max(0.0))
    }

    /// Creates an absolute pixel size, clamped to `0.0..=PX_MAX`.
    #[must_use]
    pub fn px(value: f32) -> Self {
        Self::Px(value.clamp(0.0, PX_MAX))
    }

    /// Returns the scale multiplier, or `None` when this is a pixel size.
    #[must_use]
    pub fn scale_value(self) -> Option<f32> {
        match self {
            Self::Scale(value) => Some(value),
            Self::Px(_) => None,
        }
    }

    /// Returns the pixel length, or `None` when this is a scale multiplier.
    #[must_use]
    pub fn px_value(self) -> Option<f32> {
        match self {
            Self::Px(value) => Some(value),
            Self::Scale(_) => None,
        }
    }

    /// Returns `true` when this size is zero in either form.
    #[must_use]
    pub fn is_zero(self) -> bool {
        match self {
            Self::Scale(value) | Self::Px(value) => value == 0.0,
        }
    }

    /// Resolves to a concrete pixel length.
    ///
    /// For [`Size::Px`] the value is returned as-is. For [`Size::Scale`] the
    /// result is `base * multiplier * scale`, matching how scale sizes are
    /// composed elsewhere.
    #[must_use]
    pub fn resolve_px(self, base: f32, scale: f32) -> f32 {
        match self {
            Self::Scale(multiplier) => base * multiplier * scale,
            Self::Px(value) => value,
        }
    }

    /// Resolves to pixels where a [`Size::Scale`] multiplier is a multiple of
    /// `base_rem` rem (`1rem = `[`REM_BASE_PX`]`px`): `Scale(n)` →
    /// `n * base_rem * REM_BASE_PX * scale`. [`Size::Px`] is absolute. This is
    /// the canonical resolution for rem-based size fields — callers pass the
    /// field's base in rem rather than restating the px conversion.
    #[must_use]
    pub fn resolve_rem(self, base_rem: f32, scale: f32) -> f32 {
        self.resolve_px(base_rem * REM_BASE_PX, scale)
    }

    /// Parses from a string, accepting `"24px"` or a bare number like `"1.5"`.
    #[must_use]
    pub fn parse(raw: &str) -> Option<Self> {
        let trimmed = raw.trim();
        if let Some(px) = trimmed.strip_suffix("px") {
            let value: f32 = px.trim().parse().ok()?;
            return Some(Self::px(value));
        }
        let value: f32 = trimmed.parse().ok()?;
        Some(Self::scale(value))
    }
}

impl Default for Size {
    fn default() -> Self {
        Self::Scale(1.0)
    }
}

impl Display for Size {
    fn fmt(&self, f: &mut Formatter<'_>) -> fmt::Result {
        match self {
            Self::Scale(value) => write!(f, "{value}"),
            Self::Px(value) => write!(f, "{value}px"),
        }
    }
}

impl Serialize for Size {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        match self {
            Self::Scale(value) => serializer.serialize_f32(*value),
            Self::Px(value) => serializer.serialize_str(&format!("{value}px")),
        }
    }
}

/// Untagged wire form: a number (scale) or a string (`"24px"` or a number).
#[derive(Deserialize)]
#[serde(untagged)]
enum RawSize {
    Number(f32),
    Text(String),
}

impl<'de> Deserialize<'de> for Size {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        match RawSize::deserialize(deserializer)? {
            RawSize::Number(value) => Ok(Self::scale(value)),
            RawSize::Text(text) => Self::parse(&text).ok_or(()).or_else(|()| {
                warn!("invalid size {:?}, falling back to scale 1.0", text);
                Ok(Self::default())
            }),
        }
    }
}

#[cfg(feature = "schema")]
impl schemars::JsonSchema for Size {
    fn schema_name() -> Cow<'static, str> {
        Cow::Borrowed("Size")
    }

    fn json_schema(_generator: &mut schemars::SchemaGenerator) -> schemars::Schema {
        #[cfg(feature = "schema")]
        use schemars::json_schema;

        json_schema!({
            "description": "Size as a scale multiplier (number) or absolute pixels (e.g. \"24px\")",
            "anyOf": [
                { "type": "number", "minimum": 0.0 },
                { "type": "string", "pattern": "^[0-9]+(\\.[0-9]+)?px$" }
            ]
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_scale_number() {
        let size: Size = serde_json::from_str("1.5").unwrap();
        assert_eq!(size, Size::Scale(1.5));
    }

    #[test]
    fn parses_px_string() {
        let size: Size = serde_json::from_str("\"24px\"").unwrap();
        assert_eq!(size, Size::Px(24.0));
    }

    #[test]
    fn parses_numeric_string_as_scale() {
        let size: Size = serde_json::from_str("\"2\"").unwrap();
        assert_eq!(size, Size::Scale(2.0));
    }

    #[test]
    fn invalid_string_falls_back_to_default() {
        let size: Size = serde_json::from_str("\"abc\"").unwrap();
        assert_eq!(size, Size::default());
    }

    #[test]
    fn serializes_scale_as_number_and_px_as_string() {
        assert_eq!(serde_json::to_string(&Size::Scale(1.5)).unwrap(), "1.5");
        assert_eq!(serde_json::to_string(&Size::Px(24.0)).unwrap(), "\"24px\"");
    }

    #[test]
    fn round_trips_through_string_form() {
        assert_eq!(Size::px(8.0).to_string(), "8px");
        assert_eq!(Size::scale(1.25).to_string(), "1.25");
    }

    #[test]
    fn resolve_px_applies_scale_only_to_multiplier() {
        // Scale: base * multiplier * scale
        assert_eq!(Size::Scale(2.0).resolve_px(10.0, 1.5), 30.0);
        // Px: absolute, ignores base and scale
        assert_eq!(Size::Px(24.0).resolve_px(10.0, 1.5), 24.0);
    }
}
