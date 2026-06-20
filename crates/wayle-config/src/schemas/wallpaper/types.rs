use std::fmt::{self, Display, Formatter};

use schemars::JsonSchema;
use serde::{Deserialize, Deserializer, Serialize};
use tracing::warn;
use wayle_derive::wayle_enum;

/// Image scaling mode.
#[wayle_enum(default)]
#[serde(rename_all = "lowercase")]
pub enum FitMode {
    /// Scale to cover entire display, cropping excess.
    #[default]
    Fill,
    /// Scale to fit within display, letterboxing if needed.
    Fit,
    /// Display at original size, centered.
    Center,
    /// Stretch to exactly fill, ignoring aspect ratio.
    Stretch,
}

/// Wallpaper cycling order.
#[wayle_enum(default)]
#[serde(rename_all = "lowercase")]
pub enum CyclingMode {
    /// Alphabetical order.
    #[default]
    Sequential,
    /// Random order.
    Shuffle,
}

const INTERVAL_MIN: u64 = 1;

/// Cycling interval in minutes, minimum 1.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, JsonSchema)]
#[serde(transparent)]
#[schemars(transparent)]
pub struct CyclingInterval(#[schemars(range(min = INTERVAL_MIN))] u64);

impl CyclingInterval {
    /// Default interval (15 minutes).
    pub const DEFAULT: Self = Self(15);

    /// Creates an interval, clamping to >= 1.
    #[must_use]
    pub fn new(value: u64) -> Self {
        Self(value.max(INTERVAL_MIN))
    }

    /// Returns the inner u64 value.
    #[must_use]
    pub fn value(self) -> u64 {
        self.0
    }
}

impl Default for CyclingInterval {
    fn default() -> Self {
        Self::DEFAULT
    }
}

impl Display for CyclingInterval {
    fn fmt(&self, f: &mut Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl From<u64> for CyclingInterval {
    fn from(value: u64) -> Self {
        Self::new(value)
    }
}

impl<'de> Deserialize<'de> for CyclingInterval {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let raw = u64::deserialize(deserializer)?;
        if raw < INTERVAL_MIN {
            warn!(
                "cycling interval {} below minimum ({}), clamped",
                raw, INTERVAL_MIN
            );
        }
        Ok(Self::new(raw))
    }
}

/// Per-monitor wallpaper configuration.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "kebab-case")]
pub struct MonitorWallpaperConfig {
    /// Monitor name (e.g., "HDMI-1", "DP-1").
    pub name: String,
    /// Image scaling mode for this monitor.
    #[serde(default)]
    pub fit_mode: FitMode,
    /// Wallpaper image path for this monitor.
    #[serde(default)]
    pub wallpaper: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cycling_interval_clamps_zero() {
        assert_eq!(CyclingInterval::new(0).value(), INTERVAL_MIN);
    }

    #[test]
    fn cycling_interval_preserves_valid() {
        assert_eq!(CyclingInterval::new(30).value(), 30);
    }
}
