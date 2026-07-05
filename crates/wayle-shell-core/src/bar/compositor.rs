//! Compositor detection for compositor-dependent modules.

use std::env;

/// Detected Wayland compositor.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Compositor {
    /// Hyprland compositor.
    Hyprland,
    /// niri compositor.
    Niri,
    /// MangoWM compositor.
    Mango,
    /// sway compositor.
    Sway,
    /// Unknown or unsupported compositor.
    Unknown(String),
}

impl Compositor {
    /// Detects the running Wayland compositor.
    pub fn detect() -> Self {
        if env::var("HYPRLAND_INSTANCE_SIGNATURE").is_ok() {
            return Self::Hyprland;
        }

        if env::var("NIRI_SOCKET").is_ok() {
            return Self::Niri;
        }

        if env::var("MANGO_INSTANCE_SIGNATURE").is_ok() {
            return Self::Mango;
        }

        if env::var("SWAYSOCK").is_ok() {
            return Self::Sway;
        }

        let desktop = env::var("XDG_CURRENT_DESKTOP").unwrap_or_default();
        Self::Unknown(desktop)
    }
}
