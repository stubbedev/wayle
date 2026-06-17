//! Small adapters over `hyprland` data types used by the share picker views.

use hyprland::data::{Client, Monitor};

/// Applies a monitor's rotation to its reported dimensions.
pub(super) trait MonitorTransformExt {
    /// Swaps width/height for 90°/270° rotations so the layout matches what is
    /// shown on screen.
    fn apply_transform(&mut self);
}

impl MonitorTransformExt for Monitor {
    fn apply_transform(&mut self) {
        match self.transform {
            hyprland::data::Transforms::Normal
            | hyprland::data::Transforms::Normal180
            | hyprland::data::Transforms::Flipped
            | hyprland::data::Transforms::Flipped180 => {}
            hyprland::data::Transforms::Normal90
            | hyprland::data::Transforms::Normal270
            | hyprland::data::Transforms::Flipped90
            | hyprland::data::Transforms::Flipped270 => {
                std::mem::swap(&mut self.height, &mut self.width);
            }
        }
    }
}

/// Strips shell-significant characters from window class/title strings.
pub(super) trait ClientExt {
    /// Replaces quoting/expansion characters that could break the XDPH
    /// selection string with spaces.
    fn sanitize(&mut self);
}

impl ClientExt for Client {
    fn sanitize(&mut self) {
        self.title = sanitize_string(&self.title);
        self.class = sanitize_string(&self.class);
    }
}

fn sanitize_string(target: &str) -> String {
    target
        .replace(['\'', '\"', '$', '`'], " ")
        .replace(">]", ">")
}
