//! Compositor-agnostic capture for the screenshot host.
//!
//! Region and output capture use wlr-screencopy (`OutputManager`); the output
//! transform is read straight from the `wl_output` geometry, so no
//! compositor-specific socket is involved. Window capture prefers Hyprland's
//! toplevel-export when a Hyprland handle is supplied, otherwise falls back to
//! the generic `ext-image-copy-capture` path (`ExtToplevelManager`), matching
//! the target window by app_id/title.
//!
//! All functions here block (the underlying capture drives a Wayland event loop
//! synchronously); callers run them on the GTK thread where a brief stall is
//! acceptable.

use image::RgbImage;
use wayland_client::{Connection, protocol::wl_output::WlOutput};
use wayle_share_preview::{
    buffer::Buffer,
    ext_capture::ExtToplevelManager,
    frame::FrameManager,
    image::{Image, ImageKind, Transforms},
    output::{Output, OutputManager},
};

use crate::shell::region_overlay::RegionSelection;

/// What to capture.
pub(super) enum CaptureKind {
    /// An output-relative rectangle selected via the region overlay.
    Region(RegionSelection),
    /// A whole output by connector name, or the first output when `None`.
    Output(Option<String>),
    /// A toplevel window, identified by the caller from compositor focus state.
    Window(WindowTarget),
}

/// Identifies the window to capture. The caller resolves this from whatever
/// compositor focus information is available.
#[derive(Default)]
pub(super) struct WindowTarget {
    /// Hyprland toplevel-export handle (address), when on Hyprland.
    pub(super) hyprland_handle: Option<u64>,
    /// App id / class, used to match a toplevel on the generic `ext` path.
    pub(super) app_id: Option<String>,
    /// Title, used as a secondary match key on the generic `ext` path.
    pub(super) title: Option<String>,
}

/// Captures the requested target and returns a full-resolution RGB image.
pub(super) fn capture(kind: CaptureKind) -> Result<RgbImage, String> {
    let connection =
        Connection::connect_to_env().map_err(|e| format!("cannot connect to wayland: {e}"))?;
    match kind {
        CaptureKind::Region(sel) => capture_region(&connection, &sel),
        CaptureKind::Output(name) => capture_output(&connection, name.as_deref()),
        CaptureKind::Window(target) => capture_window(&connection, &target),
    }
}

fn capture_region(connection: &Connection, sel: &RegionSelection) -> Result<RgbImage, String> {
    let mut manager = OutputManager::new(connection).map_err(|e| e.to_string())?;
    let (output, transform) = find_output(&manager, &sel.output)?;
    let buffer = manager
        .capture_output_region(&output, sel.x, sel.y, sel.width, sel.height)
        .map_err(|e| format!("region capture failed: {e}"))?;
    to_rgb(buffer, transform)
}

fn capture_output(connection: &Connection, name: Option<&str>) -> Result<RgbImage, String> {
    let mut manager = OutputManager::new(connection).map_err(|e| e.to_string())?;
    let (output, transform) = match name {
        Some(name) => find_output(&manager, name)?,
        None => manager
            .outputs
            .first()
            .map(|(wl_output, output)| (wl_output.clone(), output_transform(output)))
            .ok_or("no outputs available")?,
    };
    let buffer = manager
        .capture_output(&output)
        .map_err(|e| format!("output capture failed: {e}"))?;
    to_rgb(buffer, transform)
}

fn capture_window(connection: &Connection, target: &WindowTarget) -> Result<RgbImage, String> {
    // Hyprland fast-path: a handle plus the toplevel-export protocol.
    if let Some(handle) = target.hyprland_handle
        && let Ok(mut manager) = FrameManager::new(connection)
    {
        let buffer = manager
            .capture_frame(handle)
            .map_err(|e| format!("window capture failed: {e}"))?;
        return to_rgb(buffer, Transforms::Normal);
    }

    // Generic path: ext-foreign-toplevel-list + ext-image-copy-capture.
    let mut manager = ExtToplevelManager::new(connection)
        .map_err(|_| "window capture not supported on this compositor".to_owned())?;
    let handle = manager
        .toplevels()
        .iter()
        .find(|t| matches_target(t.app_id.as_deref(), t.title.as_deref(), target))
        .map(|t| t.handle.clone())
        .ok_or("could not find the target window to capture")?;
    let buffer = manager
        .capture_toplevel(&handle)
        .map_err(|e| format!("window capture failed: {e}"))?;
    to_rgb(buffer, Transforms::Normal)
}

/// Matches an ext toplevel against the requested target by app_id then title.
/// Requires at least one key to be present and to match.
fn matches_target(app_id: Option<&str>, title: Option<&str>, target: &WindowTarget) -> bool {
    let app_id_ok = match (target.app_id.as_deref(), app_id) {
        (Some(want), got) => got == Some(want),
        (None, _) => true,
    };
    let title_ok = match (target.title.as_deref(), title) {
        (Some(want), got) => got == Some(want),
        (None, _) => true,
    };
    // Avoid matching everything when the caller supplied no identity at all.
    (target.app_id.is_some() || target.title.is_some()) && app_id_ok && title_ok
}

/// Finds the `WlOutput` for a connector name plus its transform, cloning the
/// output so the manager stays free to borrow mutably for the capture call.
fn find_output(manager: &OutputManager, name: &str) -> Result<(WlOutput, Transforms), String> {
    manager
        .outputs
        .iter()
        .find(|(_, output)| output.name.as_deref() == Some(name))
        .map(|(wl_output, output)| (wl_output.clone(), output_transform(output)))
        .ok_or_else(|| format!("output {name} not found"))
}

/// The output's transform, read from its `wl_output` geometry.
fn output_transform(output: &Output) -> Transforms {
    output
        .geometry
        .as_ref()
        .map_or(Transforms::Normal, |geometry| geometry.transform.into())
}

/// Converts a captured XRGB buffer to an RGB image, applying the transform.
fn to_rgb(buffer: Buffer, transform: Transforms) -> Result<RgbImage, String> {
    let image = Image::new(buffer)
        .and_then(Image::into_rgb)
        .map_err(|e| format!("cannot decode capture: {e}"))?
        .transform(transform);
    match image.buffer {
        ImageKind::Rgb(rgb) => Ok(rgb),
        ImageKind::Xrgb(_) => Err("capture did not convert to rgb".to_owned()),
    }
}
