//! Compositor-agnostic output info + string helpers for the share picker.

use wayland_client::protocol::wl_output::{Transform, WlOutput};
use wayle_share_preview::output::Output;

/// An output's connector name and global logical geometry, derived from
/// `wl_output` data so it works on any compositor (no Hyprland socket).
#[derive(Clone)]
pub(super) struct OutputInfo {
    pub(super) wl_output: WlOutput,
    pub(super) name: String,
    pub(super) x: i32,
    pub(super) y: i32,
    /// Width/height with the output transform already applied (90°/270°
    /// rotations swap the axes), so layout matches what is shown.
    pub(super) width: i32,
    pub(super) height: i32,
    pub(super) scale: f32,
    pub(super) transform: Transform,
}

impl OutputInfo {
    /// Builds an [`OutputInfo`] from a screencopy `(WlOutput, Output)` pair,
    /// or `None` when the output lacks a name or mode.
    pub(super) fn from_output(wl_output: &WlOutput, output: &Output) -> Option<Self> {
        let name = output.name.clone()?;
        let mode = output.mode.as_ref()?;
        let geometry = output.geometry.as_ref();
        let transform = geometry.map_or(Transform::Normal, |g| g.transform);

        let (mut width, mut height) = (mode.width, mode.height);
        if matches!(
            transform,
            Transform::_90 | Transform::_270 | Transform::Flipped90 | Transform::Flipped270
        ) {
            std::mem::swap(&mut width, &mut height);
        }

        Some(Self {
            wl_output: wl_output.clone(),
            name,
            x: geometry.map_or(0, |g| g.x),
            y: geometry.map_or(0, |g| g.y),
            width,
            height,
            scale: output.scale.unwrap_or(1) as f32,
            transform,
        })
    }
}
