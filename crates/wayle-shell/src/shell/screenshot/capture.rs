//! Compositor-agnostic capture for the screenshot host.
//!
//! Output capture uses wlr-screencopy (`OutputManager`); the output transform
//! is read straight from the `wl_output` geometry, so no compositor-specific
//! socket is involved. Region capture is a freeze-frame crop: every output is
//! captured up front (before the region overlay maps, so transient popups are
//! preserved) and the selection is cropped from the in-memory frame. Window
//! capture prefers Hyprland's toplevel-export when a Hyprland handle is
//! supplied, otherwise falls back to the generic `ext-image-copy-capture` path
//! (`ExtToplevelManager`), matching the target window by app_id/title.
//!
//! All functions here block (the underlying capture drives a Wayland event loop
//! synchronously); callers run them on the GTK thread where a brief stall is
//! acceptable.

use std::time::Instant;

use image::RgbImage;
use tracing::info;
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
    /// A whole output by connector name, or the first output when `None`.
    Output(Option<String>),
    /// A toplevel window, identified by the caller from compositor focus state.
    Window(WindowTarget),
}

/// A whole output captured up front for the freeze-frame region flow: the
/// connector name plus the full-resolution, transform-corrected frame.
pub(super) struct FrozenOutput {
    pub(super) connector: String,
    pub(super) image: RgbImage,
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
        CaptureKind::Output(name) => capture_output(&connection, name.as_deref()),
        CaptureKind::Window(target) => capture_window(&connection, &target),
    }
}

/// Captures every output to a full-resolution frame for the freeze-frame region
/// flow. Run before the region overlay maps so any transient popups on screen
/// are baked into the frames.
#[allow(clippy::cognitive_complexity)]
pub(super) fn capture_all_outputs() -> Result<Vec<FrozenOutput>, String> {
    let setup = Instant::now();
    let connection =
        Connection::connect_to_env().map_err(|e| format!("cannot connect to wayland: {e}"))?;
    let mut manager = OutputManager::new(&connection).map_err(|e| e.to_string())?;

    let targets = snapshot_targets(&manager);
    if cfg!(debug_assertions) {
        info!(
            setup_ms = setup.elapsed().as_millis(),
            outputs = targets.len(),
            "screenshot freeze: wl manager ready"
        );
    }

    let copy = Instant::now();
    let raw = copy_outputs(&mut manager, targets)?;
    if cfg!(debug_assertions) {
        info!(
            copy_ms = copy.elapsed().as_millis(),
            "screenshot freeze: screencopy done"
        );
    }

    // Phase 2 (parallel): convert/rotate each frame off the Wayland thread.
    let convert = Instant::now();
    let frozen = convert_outputs(raw)?;
    if cfg!(debug_assertions) {
        info!(
            convert_ms = convert.elapsed().as_millis(),
            "screenshot freeze: convert done"
        );
    }
    Ok(frozen)
}

/// Snapshots the named outputs and their transforms so the manager stays free
/// to borrow mutably during capture.
fn snapshot_targets(manager: &OutputManager) -> Vec<(WlOutput, String, Transforms)> {
    manager
        .outputs
        .iter()
        .filter_map(|(wl_output, output)| {
            output
                .name
                .clone()
                .map(|name| (wl_output.clone(), name, output_transform(output)))
        })
        .collect()
}

/// Phase 1 of [`capture_all_outputs`] (Wayland thread): screencopy each output
/// and read its buffer into a `Send` [`Image`]. Sequential — it drives one
/// event loop.
fn copy_outputs(
    manager: &mut OutputManager,
    targets: Vec<(WlOutput, String, Transforms)>,
) -> Result<Vec<(String, Image, Transforms)>, String> {
    let mut raw = Vec::with_capacity(targets.len());
    for (wl_output, connector, transform) in targets {
        let buffer = manager
            .capture_output(&wl_output, false)
            .map_err(|e| format!("output capture failed: {e}"))?;
        let image = Image::new(buffer).map_err(|e| format!("cannot decode capture: {e}"))?;
        raw.push((connector, image, transform));
    }
    Ok(raw)
}

/// Phase 2 of [`capture_all_outputs`]: the XRGB→RGB conversion plus any
/// rotation is pure CPU on owned buffers, so fan it out one thread per output.
fn convert_outputs(raw: Vec<(String, Image, Transforms)>) -> Result<Vec<FrozenOutput>, String> {
    std::thread::scope(|scope| {
        let handles: Vec<_> = raw
            .into_iter()
            .map(|(connector, image, transform)| {
                scope.spawn(move || -> Result<FrozenOutput, String> {
                    let image = image
                        .into_rgb()
                        .map_err(|e| format!("cannot decode capture: {e}"))?
                        .transform(transform);
                    match image.buffer {
                        ImageKind::Rgb(rgb) => Ok(FrozenOutput {
                            connector,
                            image: rgb,
                        }),
                        ImageKind::Xrgb(_) => Err("capture did not convert to rgb".to_owned()),
                    }
                })
            })
            .collect();
        handles
            .into_iter()
            .map(|handle| {
                handle
                    .join()
                    .map_err(|_| "capture worker panicked".to_owned())?
            })
            .collect::<Result<Vec<_>, _>>()
    })
}

/// Crops the selection out of a frozen output frame. `logical` is the output's
/// logical (compositor) size; the frame is at physical resolution, so the
/// selection is scaled by the frame/logical ratio and clamped to the frame.
pub(super) fn crop_frozen(
    image: &RgbImage,
    logical_width: i32,
    logical_height: i32,
    sel: &RegionSelection,
) -> RgbImage {
    let scale_x = image.width() as f64 / f64::from(logical_width.max(1));
    let scale_y = image.height() as f64 / f64::from(logical_height.max(1));

    let x = (f64::from(sel.x) * scale_x).round() as u32;
    let y = (f64::from(sel.y) * scale_y).round() as u32;
    let w = (f64::from(sel.width) * scale_x).round() as u32;
    let h = (f64::from(sel.height) * scale_y).round() as u32;

    let x = x.min(image.width());
    let y = y.min(image.height());
    let w = w.min(image.width() - x);
    let h = h.min(image.height() - y);

    image::imageops::crop_imm(image, x, y, w, h).to_image()
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
        .capture_output(&output, false)
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
        .capture_toplevel(&handle, false)
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
