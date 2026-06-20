//! Screencopy-backed capture for the screenshot host.
//!
//! Reuses `wayle-share-preview`'s wlr-screencopy (`OutputManager`) and Hyprland
//! toplevel-export (`FrameManager`) capture, then converts to a full-resolution
//! `RgbImage`. All functions here block (the underlying capture drives a
//! Wayland event loop synchronously); callers run them on the GTK thread where
//! a brief stall is acceptable.

use hyprland::{
    data::{Client, Monitors},
    shared::{HyprData, HyprDataActiveOptional},
};
use image::RgbImage;
use wayland_client::{Connection, protocol::wl_output::WlOutput};
use wayle_share_preview::{
    buffer::Buffer,
    frame::FrameManager,
    image::{Image, ImageKind, Transforms},
    output::OutputManager,
};

use crate::shell::region_overlay::RegionSelection;

/// What to capture.
pub(super) enum CaptureKind {
    /// An output-relative rectangle selected via the region overlay.
    Region(RegionSelection),
    /// A whole output by connector name, or the focused output when `None`.
    Output(Option<String>),
    /// A toplevel by Hyprland handle, or the active window when `None`.
    Window(Option<u64>),
}

/// Captures the requested target and returns a full-resolution RGB image.
pub(super) fn capture(kind: CaptureKind) -> Result<RgbImage, String> {
    let connection =
        Connection::connect_to_env().map_err(|e| format!("cannot connect to wayland: {e}"))?;
    match kind {
        CaptureKind::Region(sel) => capture_region(&connection, &sel),
        CaptureKind::Output(name) => capture_output(&connection, name),
        CaptureKind::Window(handle) => capture_window(&connection, handle),
    }
}

fn capture_region(connection: &Connection, sel: &RegionSelection) -> Result<RgbImage, String> {
    let mut manager = OutputManager::new(connection).map_err(|e| e.to_string())?;
    let output = find_output(&manager, &sel.output)?;
    let buffer = manager
        .capture_output_region(&output, sel.x, sel.y, sel.width, sel.height)
        .map_err(|e| format!("region capture failed: {e}"))?;
    to_rgb(buffer, monitor_transform(&sel.output))
}

fn capture_output(connection: &Connection, name: Option<String>) -> Result<RgbImage, String> {
    let name = match name {
        Some(name) => name,
        None => focused_monitor().ok_or("no focused output found")?,
    };
    let mut manager = OutputManager::new(connection).map_err(|e| e.to_string())?;
    let output = find_output(&manager, &name)?;
    let buffer = manager
        .capture_output(&output)
        .map_err(|e| format!("output capture failed: {e}"))?;
    to_rgb(buffer, monitor_transform(&name))
}

fn capture_window(connection: &Connection, handle: Option<u64>) -> Result<RgbImage, String> {
    let (handle, transform) = match handle {
        Some(handle) => (handle, Transforms::Normal),
        None => {
            let client = Client::get_active()
                .map_err(|e| format!("cannot query active window: {e}"))?
                .ok_or("no active window")?;
            let address = format!("{}", client.address);
            let handle = u64::from_str_radix(address.trim_start_matches("0x"), 16)
                .map_err(|e| format!("invalid window address: {e}"))?;
            let transform = client
                .monitor
                .and_then(|id| Monitors::get().ok().and_then(|ms| ms.into_iter().find(|m| m.id == id)))
                .map_or(Transforms::Normal, |m| m.transform.into());
            (handle, transform)
        }
    };
    let mut manager = FrameManager::new(connection).map_err(|e| e.to_string())?;
    let buffer = manager
        .capture_frame(handle)
        .map_err(|e| format!("window capture failed: {e}"))?;
    to_rgb(buffer, transform)
}

/// Finds the `WlOutput` for a connector name, cloning it so the manager stays
/// free to borrow mutably for the capture call.
fn find_output(manager: &OutputManager, name: &str) -> Result<WlOutput, String> {
    manager
        .outputs
        .iter()
        .find(|(_, output)| output.name.as_deref() == Some(name))
        .map(|(wl_output, _)| wl_output.clone())
        .ok_or_else(|| format!("output {name} not found"))
}

/// Converts a captured XRGB buffer to an RGB image, applying the output
/// transform (matching how the share picker renders captures).
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

/// Connector name of the Hyprland focused monitor, if any.
fn focused_monitor() -> Option<String> {
    Monitors::get()
        .ok()?
        .into_iter()
        .find(|m| m.focused)
        .map(|m| m.name)
}

/// Output transform for a connector name, defaulting to `Normal`.
fn monitor_transform(name: &str) -> Transforms {
    Monitors::get()
        .ok()
        .and_then(|ms| ms.into_iter().find(|m| m.name == name))
        .map_or(Transforms::Normal, |m| m.transform.into())
}
