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
    info!(
        setup_ms = setup.elapsed().as_millis(),
        outputs = targets.len(),
        "screenshot freeze: wl manager ready"
    );

    let copy = Instant::now();
    let raw = copy_outputs(&mut manager, targets)?;
    info!(
        copy_ms = copy.elapsed().as_millis(),
        "screenshot freeze: screencopy done"
    );

    // Phase 2 (parallel): convert/rotate each frame off the Wayland thread.
    let convert = Instant::now();
    let frozen = convert_outputs(raw)?;
    info!(
        convert_ms = convert.elapsed().as_millis(),
        "screenshot freeze: convert done"
    );
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
            .capture_output(&wl_output)
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

/// One output's logical placement in the compositor layout, paired with its
/// physical capture frame. Used to composite the whole multi-monitor space into
/// a single image.
pub(super) struct PlacedFrame<'a> {
    /// Logical x/y/width/height from the compositor layout (`wl_output`/GDK).
    pub(super) logical: LogicalGeometry,
    /// The physical-resolution captured frame for this output.
    pub(super) image: &'a RgbImage,
}

/// An output's logical geometry in compositor coordinates: position plus size.
/// Position may be negative (outputs left of / above the origin).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) struct LogicalGeometry {
    pub(super) x: i32,
    pub(super) y: i32,
    pub(super) width: i32,
    pub(super) height: i32,
}

/// The bounding box (in logical coordinates) that spans every output's
/// geometry: the minimum origin and the total extent. Returns `None` when there
/// are no outputs. Handles outputs at negative coordinates and arbitrary layout.
pub(super) fn layout_bounds(geometries: &[LogicalGeometry]) -> Option<(i32, i32, u32, u32)> {
    let mut iter = geometries.iter();
    let first = iter.next()?;
    let mut min_x = first.x;
    let mut min_y = first.y;
    let mut max_x = first.x + first.width;
    let mut max_y = first.y + first.height;
    for g in iter {
        min_x = min_x.min(g.x);
        min_y = min_y.min(g.y);
        max_x = max_x.max(g.x + g.width);
        max_y = max_y.max(g.y + g.height);
    }
    let width = (max_x - min_x).max(0) as u32;
    let height = (max_y - min_y).max(0) as u32;
    Some((min_x, min_y, width, height))
}

/// Composites every output's physical frame into one image spanning the whole
/// layout's logical bounding box.
///
/// Each output's frame is at physical resolution (which can differ from its
/// logical size under fractional scaling), so it is scaled to its logical size
/// and placed at its logical origin minus the layout's top-left. With a single
/// output this reduces to that output's frame scaled to its logical size.
/// Returns a 0x0 image when there are no outputs.
pub(super) fn composite_outputs(frames: &[PlacedFrame]) -> RgbImage {
    let geometries: Vec<LogicalGeometry> = frames.iter().map(|f| f.logical).collect();
    let Some((origin_x, origin_y, width, height)) = layout_bounds(&geometries) else {
        return RgbImage::new(0, 0);
    };

    let mut canvas = RgbImage::new(width, height);
    for frame in frames {
        let logical_w = frame.logical.width.max(0) as u32;
        let logical_h = frame.logical.height.max(0) as u32;
        if logical_w == 0 || logical_h == 0 {
            continue;
        }
        // Scale the physical frame to the output's logical size so every output
        // shares the layout's logical coordinate space.
        let scaled = if frame.image.width() == logical_w && frame.image.height() == logical_h {
            std::borrow::Cow::Borrowed(frame.image)
        } else {
            std::borrow::Cow::Owned(image::imageops::resize(
                frame.image,
                logical_w,
                logical_h,
                image::imageops::FilterType::Triangle,
            ))
        };
        let dst_x = (frame.logical.x - origin_x).max(0) as u32;
        let dst_y = (frame.logical.y - origin_y).max(0) as u32;
        image::imageops::replace(
            &mut canvas,
            scaled.as_ref(),
            i64::from(dst_x),
            i64::from(dst_y),
        );
    }
    canvas
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

#[cfg(test)]
mod tests {
    use image::{Rgb, RgbImage};

    use super::{LogicalGeometry, PlacedFrame, composite_outputs, layout_bounds};

    fn geom(x: i32, y: i32, width: i32, height: i32) -> LogicalGeometry {
        LogicalGeometry {
            x,
            y,
            width,
            height,
        }
    }

    /// A solid-color frame of the given physical resolution.
    fn solid(width: u32, height: u32, color: [u8; 3]) -> RgbImage {
        RgbImage::from_pixel(width, height, Rgb(color))
    }

    #[test]
    fn bounds_none_for_empty() {
        assert_eq!(layout_bounds(&[]), None);
    }

    #[test]
    fn bounds_single_output() {
        assert_eq!(
            layout_bounds(&[geom(0, 0, 1920, 1080)]),
            Some((0, 0, 1920, 1080))
        );
    }

    #[test]
    fn bounds_side_by_side() {
        // 1920+2560 wide, tallest is 1440.
        assert_eq!(
            layout_bounds(&[geom(0, 0, 1920, 1080), geom(1920, 0, 2560, 1440)]),
            Some((0, 0, 4480, 1440))
        );
    }

    #[test]
    fn bounds_negative_origin() {
        // Secondary output placed to the left of and above the origin.
        assert_eq!(
            layout_bounds(&[geom(0, 0, 1920, 1080), geom(-1280, -200, 1280, 1024)]),
            Some((-1280, -200, 3200, 1280))
        );
    }

    #[test]
    fn bounds_stacked_with_gap() {
        // Vertical layout where the second output starts below the first.
        assert_eq!(
            layout_bounds(&[geom(0, 0, 1000, 500), geom(0, 600, 800, 400)]),
            Some((0, 0, 1000, 1000))
        );
    }

    #[test]
    fn composite_empty_is_zero_sized() {
        let image = composite_outputs(&[]);
        assert_eq!((image.width(), image.height()), (0, 0));
    }

    #[test]
    fn composite_single_output_matches_logical_size() {
        let frame = solid(1920, 1080, [10, 20, 30]);
        let image = composite_outputs(&[PlacedFrame {
            logical: geom(0, 0, 1920, 1080),
            image: &frame,
        }]);
        assert_eq!((image.width(), image.height()), (1920, 1080));
        assert_eq!(*image.get_pixel(0, 0), Rgb([10, 20, 30]));
        assert_eq!(*image.get_pixel(1919, 1079), Rgb([10, 20, 30]));
    }

    #[test]
    fn composite_scales_physical_to_logical() {
        // A HiDPI output: 2x physical resolution over a smaller logical size.
        let frame = solid(2000, 2000, [200, 100, 50]);
        let image = composite_outputs(&[PlacedFrame {
            logical: geom(0, 0, 1000, 1000),
            image: &frame,
        }]);
        assert_eq!((image.width(), image.height()), (1000, 1000));
        assert_eq!(*image.get_pixel(500, 500), Rgb([200, 100, 50]));
    }

    #[test]
    fn composite_places_outputs_side_by_side() {
        let left = solid(1920, 1080, [255, 0, 0]);
        let right = solid(2560, 1440, [0, 255, 0]);
        let image = composite_outputs(&[
            PlacedFrame {
                logical: geom(0, 0, 1920, 1080),
                image: &left,
            },
            PlacedFrame {
                logical: geom(1920, 0, 2560, 1440),
                image: &right,
            },
        ]);
        assert_eq!((image.width(), image.height()), (4480, 1440));
        // Left output occupies the top-left region.
        assert_eq!(*image.get_pixel(0, 0), Rgb([255, 0, 0]));
        assert_eq!(*image.get_pixel(1919, 1079), Rgb([255, 0, 0]));
        // Right output begins exactly at its logical x offset.
        assert_eq!(*image.get_pixel(1920, 0), Rgb([0, 255, 0]));
        assert_eq!(*image.get_pixel(4479, 1439), Rgb([0, 255, 0]));
    }

    #[test]
    fn composite_handles_negative_origin_offsets() {
        // The primary output sits at the origin; a secondary output is up-left at
        // negative coordinates, which must map to the canvas top-left.
        let primary = solid(1920, 1080, [1, 2, 3]);
        let secondary = solid(1280, 1024, [4, 5, 6]);
        let image = composite_outputs(&[
            PlacedFrame {
                logical: geom(0, 0, 1920, 1080),
                image: &primary,
            },
            PlacedFrame {
                logical: geom(-1280, -200, 1280, 1024),
                image: &secondary,
            },
        ]);
        // Bounding box: x in [-1280, 1920], y in [-200, 1080].
        assert_eq!((image.width(), image.height()), (3200, 1280));
        // Secondary output anchored at canvas (0, 0).
        assert_eq!(*image.get_pixel(0, 0), Rgb([4, 5, 6]));
        // Primary output at logical (0, 0) -> canvas (1280, 200).
        assert_eq!(*image.get_pixel(1280, 200), Rgb([1, 2, 3]));
    }

    #[test]
    fn composite_skips_zero_sized_logical() {
        // An output reporting a zero logical dimension must not panic or paint.
        let frame = solid(100, 100, [9, 9, 9]);
        let image = composite_outputs(&[PlacedFrame {
            logical: geom(0, 0, 0, 0),
            image: &frame,
        }]);
        assert_eq!((image.width(), image.height()), (0, 0));
    }
}
