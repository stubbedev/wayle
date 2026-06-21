//! Click action types for bar module interaction.

use serde::{Deserialize, Serialize};

/// Action to perform on a bar module click or scroll event.
///
/// Serializes to/from a string for TOML config compatibility:
/// - `""` -> `None`
/// - `"dropdown:audio"` -> `Dropdown("audio")`
/// - `"brightness:+5"` -> `Brightness(5)`
/// - `"brightness:toggle"` -> `BrightnessToggle`
/// - `"pavucontrol"` -> `Shell("pavucontrol")`
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub enum ClickAction {
    /// Open a named dropdown panel.
    Dropdown(String),
    /// Execute a shell command.
    Shell(String),
    /// Adjust backlight brightness by a percentage delta (may be negative),
    /// handled natively without shelling out to an external tool. The result
    /// is floored at the module's configured minimum so a dimmer can never
    /// scroll fully dark.
    Brightness(i32),
    /// Toggle backlight between fully dark and the last non-zero brightness,
    /// handled natively. Unlike [`Brightness`](Self::Brightness) this is not
    /// floored — it is the intentional way to reach 0%.
    BrightnessToggle,
    #[default]
    /// No action configured.
    None,
}

impl ClickAction {
    fn from_str(s: &str) -> Self {
        if s.is_empty() {
            return Self::None;
        }
        if let Some(rest) = s.strip_prefix("brightness:") {
            return match rest {
                "toggle" => Self::BrightnessToggle,
                // Malformed delta is a no-op rather than a bogus shell-out.
                _ => rest.parse::<i32>().map_or(Self::None, Self::Brightness),
            };
        }
        match s.strip_prefix("dropdown:") {
            Some(name) => Self::Dropdown(name.to_owned()),
            None => Self::Shell(s.to_owned()),
        }
    }
}

impl Serialize for ClickAction {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        match self {
            Self::Dropdown(name) => serializer.serialize_str(&format!("dropdown:{name}")),
            Self::Shell(cmd) => serializer.serialize_str(cmd),
            Self::Brightness(delta) => serializer.serialize_str(&format!("brightness:{delta}")),
            Self::BrightnessToggle => serializer.serialize_str("brightness:toggle"),
            Self::None => serializer.serialize_str(""),
        }
    }
}

impl<'de> Deserialize<'de> for ClickAction {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let s = String::deserialize(deserializer)?;
        Ok(Self::from_str(&s))
    }
}

impl schemars::JsonSchema for ClickAction {
    fn schema_name() -> std::borrow::Cow<'static, str> {
        std::borrow::Cow::Borrowed("ClickAction")
    }

    fn json_schema(generator: &mut schemars::SchemaGenerator) -> schemars::Schema {
        String::json_schema(generator)
    }
}
