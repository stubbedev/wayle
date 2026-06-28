//! ScreenCast source types and picker-selection parsing (pure logic).

use wayland_client::protocol::{wl_output::Transform, wl_shm::Format as ShmFormat};

/// Maps a `wl_output` transform to the SPA `spa_meta_videotransform_value`
/// written into a `SPA_META_VideoTransform` meta block, so a rotated/flipped
/// monitor streams the right way up instead of sideways.
///
/// The two enums share the same ordering and meaning, so this is a direct
/// 1:1 mapping: `Normal=0`, `_90=1`, `_180=2`, `_270=3`, `Flipped=4`,
/// `Flipped90=5`, `Flipped180=6`, `Flipped270=7`. `wl_output::Transform` is
/// `#[non_exhaustive]`, so any value we don't recognise falls back to the
/// identity transform (0) — streaming un-rotated is always safe, just not
/// always correct.
#[must_use]
pub fn spa_video_transform(t: Transform) -> u32 {
    match t {
        Transform::Normal => 0,
        Transform::_90 => 1,
        Transform::_180 => 2,
        Transform::_270 => 3,
        Transform::Flipped => 4,
        Transform::Flipped90 => 5,
        Transform::Flipped180 => 6,
        Transform::Flipped270 => 7,
        _ => 0,
    }
}

/// Clamps a list of damage rectangles to the frame bounds for the
/// `SPA_META_VideoDamage` meta block.
///
/// - rectangles are clamped so they never extend past `width`/`height`,
/// - rectangles that are empty (or fall fully outside the frame) after
///   clamping are dropped,
/// - if the input is empty (no damage reported, or window capture which never
///   reports damage) the whole frame is reported as damaged — a correct,
///   conservative fallback that simply forgoes the "re-encode only the delta"
///   optimisation for that frame,
/// - the result is capped at `max` entries; if there are more damage rects than
///   slots the whole frame is reported instead (matching xdph's behaviour when
///   its fixed damage array overflows).
///
/// Rectangles are `(x, y, w, h)`.
///
/// Production uses [`clamp_damage_into`] (reused scratch buffer); this
/// allocating wrapper exists for the unit tests.
#[cfg(test)]
#[must_use]
pub fn clamp_damage(
    rects: &[(u32, u32, u32, u32)],
    width: u32,
    height: u32,
    max: usize,
) -> Vec<(u32, u32, u32, u32)> {
    let mut out = Vec::new();
    clamp_damage_into(&mut out, rects, width, height, max);
    out
}

/// Like [`clamp_damage`] but writes into a caller-owned buffer, clearing it
/// first. Lets the per-frame producer reuse one scratch `Vec` instead of
/// allocating every frame.
pub fn clamp_damage_into(
    out: &mut Vec<(u32, u32, u32, u32)>,
    rects: &[(u32, u32, u32, u32)],
    width: u32,
    height: u32,
    max: usize,
) {
    out.clear();
    let full = |out: &mut Vec<(u32, u32, u32, u32)>| {
        out.clear();
        out.push((0, 0, width, height));
    };

    if rects.is_empty() || max == 0 {
        return full(out);
    }

    for &(x, y, w, h) in rects {
        // Drop anything starting outside the frame.
        if x >= width || y >= height {
            continue;
        }
        let cw = w.min(width - x);
        let ch = h.min(height - y);
        if cw == 0 || ch == 0 {
            continue;
        }
        out.push((x, y, cw, ch));
    }

    // No usable damage survived clamping, or more than we can carry -> report the
    // whole frame rather than silently truncating real damage.
    if out.is_empty() || out.len() > max {
        full(out);
    }
}

/// Picks the order of dmabuf modifiers to try allocating with, given the
/// modifiers the consumer advertised for the chosen DRM format in its PipeWire
/// `EnumFormat` (the wlr-screencopy `linux_dmabuf` event only tells us the
/// format, not the modifier list).
///
/// Mirrors xdph's strategy (`Screencopy.cpp` `pwStreamParamChanged`): prefer
/// the explicit modifiers the consumer named, then fall back to
/// `DRM_FORMAT_MOD_INVALID` (let the driver choose an implicit modifier) and
/// finally `DRM_FORMAT_MOD_LINEAR`. Kept pure so the branching is testable; the
/// actual gbm allocation in `pipewire.rs` walks this list and stops at the
/// first success.
///
/// Never returns empty — the caller always has at least `INVALID` to try before
/// giving up on dmabuf and falling back to SHM.
///
/// Currently unused by the producer (which offers exactly the modifier the bo
/// was allocated with, so no candidate ordering is needed); kept and unit-tested
/// as the reference strategy for a future offer-many/let-consumer-pick path.
#[cfg(test)]
#[must_use]
pub fn dmabuf_modifier_candidates(advertised: &[u64]) -> Vec<u64> {
    /// `DRM_FORMAT_MOD_INVALID` — "let the driver choose an implicit modifier".
    const MOD_INVALID: u64 = 0x00ff_ffff_ffff_ffff;
    /// `DRM_FORMAT_MOD_LINEAR` — plain linear layout, importable everywhere.
    const MOD_LINEAR: u64 = 0;

    let mut out: Vec<u64> = Vec::new();
    // Prefer explicit modifiers the consumer named, in advertised order,
    // skipping duplicates.
    for &m in advertised {
        if !out.contains(&m) {
            out.push(m);
        }
    }
    // Always end with the universally-safe fallbacks so we never return empty
    // and always have a last resort before SHM.
    if !out.contains(&MOD_INVALID) {
        out.push(MOD_INVALID);
    }
    if !out.contains(&MOD_LINEAR) {
        out.push(MOD_LINEAR);
    }
    out
}

/// Pixel layout of a captured frame, mapped from the `wl_shm` format the
/// compositor handed back. Kept compositor-agnostic so the PipeWire producer
/// can translate it to the matching SPA video format instead of assuming BGRx.
///
/// `wl_shm` formats are little-endian packed, so the in-memory byte order is
/// the reverse of the name: `Xrgb8888` is `B,G,R,x` in memory == SPA `BGRx`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PixelFormat {
    /// `wl_shm` `Xrgb8888` — the common wlr-screencopy output.
    Bgrx,
    /// `wl_shm` `Argb8888`.
    Bgra,
    /// `wl_shm` `Xbgr8888`.
    Rgbx,
    /// `wl_shm` `Abgr8888`.
    Rgba,
}

impl PixelFormat {
    /// Maps a `wl_shm` format to its SPA-equivalent pixel layout, or `None` if
    /// it is not a 32-bit packed format we can stream.
    #[must_use]
    pub fn from_wl(format: ShmFormat) -> Option<Self> {
        Some(match format {
            ShmFormat::Xrgb8888 => Self::Bgrx,
            ShmFormat::Argb8888 => Self::Bgra,
            ShmFormat::Xbgr8888 => Self::Rgbx,
            ShmFormat::Abgr8888 => Self::Rgba,
            _ => return None,
        })
    }
}

/// Clamps a requested frame rate to the output's refresh rate so a 30 fps
/// default does not oversample a 24 Hz source nor cap a 144 Hz one below its
/// true ceiling. `refresh_mhz` is the `wl_output` mode refresh in millihertz;
/// `None` (e.g. window capture, unknown mode) leaves the request unchanged.
/// Never returns 0.
#[must_use]
pub fn effective_fps(requested: u32, refresh_mhz: Option<i32>) -> u32 {
    let capped = match refresh_mhz {
        Some(mhz) if mhz > 0 => {
            #[allow(clippy::cast_sign_loss)]
            let hz = (mhz as u32) / 1000;
            requested.min(hz.max(1))
        }
        _ => requested,
    };
    capped.max(1)
}

/// What kind of content a stream captures. Bitmask values match the portal
/// spec (and `AvailableSourceTypes`).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SourceType {
    /// A whole monitor / output.
    Monitor,
    /// A single application window (toplevel).
    Window,
    /// A virtual/region source not tied to one output.
    Virtual,
}

impl SourceType {
    /// The single-bit mask for this source type.
    #[must_use]
    pub fn bit(self) -> u32 {
        match self {
            Self::Monitor => 1,
            Self::Window => 2,
            Self::Virtual => 4,
        }
    }
}

/// Cursor handling mode. Bitmask values match the portal spec.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CursorMode {
    /// Cursor is not captured.
    Hidden,
    /// Cursor is composited into the frames.
    Embedded,
    /// Cursor is delivered as stream metadata.
    Metadata,
}

impl CursorMode {
    /// Parses the portal `cursor_mode` bitmask, defaulting to [`Self::Hidden`].
    #[must_use]
    pub fn from_bits(bits: u32) -> Self {
        if bits & 4 != 0 {
            Self::Metadata
        } else if bits & 2 != 0 {
            Self::Embedded
        } else {
            Self::Hidden
        }
    }

    /// Whether the cursor should be visible in the captured frames.
    #[must_use]
    pub fn show_cursor(self) -> bool {
        matches!(self, Self::Embedded | Self::Metadata)
    }
}

/// A resolved capture target chosen by the user in the picker.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CaptureTarget {
    /// Capture a whole output by `wl_output` name.
    Output(String),
    /// Capture a toplevel by its stable `ext_foreign_toplevel` identifier.
    Window(String),
    /// Capture a rectangular region of an output.
    Region {
        /// Output name the region is on.
        output: String,
        /// Region left in output-local pixels.
        x: i32,
        /// Region top in output-local pixels.
        y: i32,
        /// Region width in pixels.
        width: i32,
        /// Region height in pixels.
        height: i32,
    },
}

impl CaptureTarget {
    /// The portal source type this target reports as.
    #[must_use]
    pub fn source_type(&self) -> SourceType {
        match self {
            Self::Output(_) => SourceType::Monitor,
            Self::Window(_) => SourceType::Window,
            Self::Region { .. } => SourceType::Virtual,
        }
    }

    /// Serializes the target to the same `screen:`/`window:`/`region:` payload
    /// the picker emits. Round-trips with [`parse_target`]; used to persist a
    /// selection inside a restore token.
    #[must_use]
    pub fn to_payload(&self) -> String {
        match self {
            Self::Output(name) => format!("screen:{name}"),
            Self::Window(ident) => format!("window:{ident}"),
            Self::Region {
                output,
                x,
                y,
                width,
                height,
            } => format!("region:{output}@{x},{y},{width},{height}"),
        }
    }

    /// Parses a payload produced by [`Self::to_payload`].
    #[must_use]
    pub fn from_payload(payload: &str) -> Option<Self> {
        parse_target(payload)
    }
}

/// The parsed result of a [`SharePicker`] reply.
///
/// [`SharePicker`]: wayle_ipc::share_picker
///
/// Single-target form, superseded in production by the multi-target
/// [`parse_picker_reply_multi`]; retained for the parser unit tests.
#[cfg(test)]
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PickerSelection {
    /// Whether the user opted to allow a restore token.
    pub allow_token: bool,
    /// The chosen capture target.
    pub target: CaptureTarget,
}

/// Parses a `com.wayle.SharePicker1.pick` reply.
///
/// The reply is the XDPH selection suffix: an optional leading flag segment
/// (`r` = allow restore token) before the first `/`, then a
/// `screen:`/`window:`/`region:` payload. An empty string means the user
/// cancelled.
///
/// Returns `None` on cancel or a malformed reply.
#[cfg(test)]
#[must_use]
pub fn parse_picker_reply(reply: &str) -> Option<PickerSelection> {
    if reply.is_empty() {
        return None;
    }
    let slash = reply.find('/')?;
    let flags = &reply[..slash];
    let payload = &reply[slash + 1..];
    let allow_token = flags.contains('r');
    let target = parse_target(payload)?;
    Some(PickerSelection {
        allow_token,
        target,
    })
}

/// Separator between targets in a multi-select picker reply.
///
/// Chosen because it never appears in a single target payload: outputs and
/// window identifiers use `:` and `@`, regions use `:`, `@` and `,`, and the
/// flag segment is delimited by the first `/`. A `;` therefore unambiguously
/// joins several `screen:`/`window:`/`region:` payloads.
const TARGET_SEPARATOR: char = ';';

/// Parses a multi-select `com.wayle.SharePicker1.pick` reply.
///
/// Like [`parse_picker_reply`], the reply is an optional leading flag segment
/// (`r` = allow restore token) before the first `/`, then one or more
/// `screen:`/`window:`/`region:` payloads joined by [`TARGET_SEPARATOR`]. An
/// empty reply means the user cancelled.
///
/// Returns `Some((allow_token, targets))` with at least one target, or `None`
/// on cancel or if any payload is empty or malformed.
#[must_use]
pub fn parse_picker_reply_multi(reply: &str) -> Option<(bool, Vec<CaptureTarget>)> {
    if reply.is_empty() {
        return None;
    }
    let slash = reply.find('/')?;
    let flags = &reply[..slash];
    let payload = &reply[slash + 1..];
    let allow_token = flags.contains('r');

    let mut targets = Vec::new();
    for piece in payload.split(TARGET_SEPARATOR) {
        targets.push(parse_target(piece)?);
    }
    if targets.is_empty() {
        return None;
    }
    Some((allow_token, targets))
}

/// Parses the `screen:`/`window:`/`region:` payload into a [`CaptureTarget`].
fn parse_target(payload: &str) -> Option<CaptureTarget> {
    if let Some(name) = payload.strip_prefix("screen:") {
        return (!name.is_empty()).then(|| CaptureTarget::Output(name.to_owned()));
    }
    if let Some(ident) = payload.strip_prefix("window:") {
        return (!ident.is_empty()).then(|| CaptureTarget::Window(ident.to_owned()));
    }
    if let Some(spec) = payload.strip_prefix("region:") {
        return parse_region(spec);
    }
    None
}

/// Parses `OUTPUT@x,y,w,h`.
fn parse_region(spec: &str) -> Option<CaptureTarget> {
    let (output, rect) = spec.split_once('@')?;
    if output.is_empty() {
        return None;
    }
    let mut nums = rect.split(',');
    let x = nums.next()?.parse().ok()?;
    let y = nums.next()?.parse().ok()?;
    let width = nums.next()?.parse().ok()?;
    let height = nums.next()?.parse().ok()?;
    if nums.next().is_some() || width <= 0 || height <= 0 {
        return None;
    }
    Some(CaptureTarget::Region {
        output: output.to_owned(),
        x,
        y,
        width,
        height,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_screen() {
        let sel = parse_picker_reply("/screen:DP-1").unwrap();
        assert!(!sel.allow_token);
        assert_eq!(sel.target, CaptureTarget::Output("DP-1".to_owned()));
        assert_eq!(sel.target.source_type(), SourceType::Monitor);
    }

    #[test]
    fn parses_window_with_token() {
        let sel = parse_picker_reply("r/window:firefox@instance-3").unwrap();
        assert!(sel.allow_token);
        assert_eq!(
            sel.target,
            CaptureTarget::Window("firefox@instance-3".to_owned())
        );
    }

    #[test]
    fn parses_region() {
        let sel = parse_picker_reply("/region:DP-1@10,20,800,600").unwrap();
        assert_eq!(
            sel.target,
            CaptureTarget::Region {
                output: "DP-1".to_owned(),
                x: 10,
                y: 20,
                width: 800,
                height: 600,
            }
        );
        assert_eq!(sel.target.source_type(), SourceType::Virtual);
    }

    #[test]
    fn empty_is_cancel() {
        assert_eq!(parse_picker_reply(""), None);
    }

    #[test]
    fn rejects_malformed() {
        assert_eq!(parse_picker_reply("garbage"), None);
        assert_eq!(parse_picker_reply("/screen:"), None);
        assert_eq!(parse_picker_reply("/window:"), None);
        assert_eq!(parse_picker_reply("/region:DP-1@1,2,3"), None);
        assert_eq!(parse_picker_reply("/region:DP-1@1,2,0,5"), None);
        assert_eq!(parse_picker_reply("/region:@1,2,3,4"), None);
        assert_eq!(parse_picker_reply("/bogus:x"), None);
    }

    #[test]
    fn multi_parses_single_target() {
        let (allow_token, targets) = parse_picker_reply_multi("/screen:DP-1").unwrap();
        assert!(!allow_token);
        assert_eq!(targets, vec![CaptureTarget::Output("DP-1".to_owned())]);
    }

    #[test]
    fn multi_parses_several_targets_with_token() {
        let (allow_token, targets) =
            parse_picker_reply_multi("r/screen:DP-1;screen:HDMI-A-1").unwrap();
        assert!(allow_token);
        assert_eq!(
            targets,
            vec![
                CaptureTarget::Output("DP-1".to_owned()),
                CaptureTarget::Output("HDMI-A-1".to_owned()),
            ]
        );
    }

    #[test]
    fn multi_parses_mixed_monitor_window_region() {
        let (allow_token, targets) = parse_picker_reply_multi(
            "/screen:DP-1;window:firefox@instance-3;region:HDMI-A-1@10,20,800,600",
        )
        .unwrap();
        assert!(!allow_token);
        assert_eq!(
            targets,
            vec![
                CaptureTarget::Output("DP-1".to_owned()),
                CaptureTarget::Window("firefox@instance-3".to_owned()),
                CaptureTarget::Region {
                    output: "HDMI-A-1".to_owned(),
                    x: 10,
                    y: 20,
                    width: 800,
                    height: 600,
                },
            ]
        );
    }

    #[test]
    fn multi_empty_is_cancel() {
        assert_eq!(parse_picker_reply_multi(""), None);
    }

    #[test]
    fn multi_rejects_malformed() {
        // No payload after the flags segment.
        assert_eq!(parse_picker_reply_multi("r/"), None);
        // No leading slash at all.
        assert_eq!(parse_picker_reply_multi("garbage"), None);
        // A single bad target fails the whole parse.
        assert_eq!(parse_picker_reply_multi("/screen:DP-1;bogus:x"), None);
        // An empty piece between separators is rejected.
        assert_eq!(parse_picker_reply_multi("/screen:DP-1;;screen:DP-2"), None);
        assert_eq!(parse_picker_reply_multi("/screen:DP-1;"), None);
        // A malformed region fails the whole parse.
        assert_eq!(
            parse_picker_reply_multi("/screen:DP-1;region:DP-2@1,2,0,5"),
            None
        );
    }

    #[test]
    fn cursor_mode_bits() {
        assert_eq!(CursorMode::from_bits(1), CursorMode::Hidden);
        assert_eq!(CursorMode::from_bits(2), CursorMode::Embedded);
        assert_eq!(CursorMode::from_bits(4), CursorMode::Metadata);
        assert_eq!(CursorMode::from_bits(6), CursorMode::Metadata);
        assert!(!CursorMode::Hidden.show_cursor());
        assert!(CursorMode::Embedded.show_cursor());
    }

    #[test]
    fn source_bits() {
        assert_eq!(SourceType::Monitor.bit(), 1);
        assert_eq!(SourceType::Window.bit(), 2);
        assert_eq!(SourceType::Virtual.bit(), 4);
    }

    #[test]
    fn pixel_format_maps_known_wl_shm_formats() {
        assert_eq!(
            PixelFormat::from_wl(ShmFormat::Xrgb8888),
            Some(PixelFormat::Bgrx)
        );
        assert_eq!(
            PixelFormat::from_wl(ShmFormat::Argb8888),
            Some(PixelFormat::Bgra)
        );
        assert_eq!(
            PixelFormat::from_wl(ShmFormat::Xbgr8888),
            Some(PixelFormat::Rgbx)
        );
        assert_eq!(
            PixelFormat::from_wl(ShmFormat::Abgr8888),
            Some(PixelFormat::Rgba)
        );
        // A planar/unsupported format is rejected so the caller can fall back.
        assert_eq!(PixelFormat::from_wl(ShmFormat::Nv12), None);
    }

    #[test]
    fn effective_fps_clamps_to_refresh() {
        // 144 Hz monitor, 30 fps request -> 30.
        assert_eq!(effective_fps(30, Some(144_000)), 30);
        // 24 Hz source, 30 fps request -> clamped to 24.
        assert_eq!(effective_fps(30, Some(24_000)), 24);
        // Unknown refresh (window capture) -> unchanged.
        assert_eq!(effective_fps(30, None), 30);
        // Never returns 0.
        assert_eq!(effective_fps(0, Some(60_000)), 1);
        assert_eq!(effective_fps(30, Some(0)), 30);
        // Sub-1 Hz refresh floors to 1, not 0.
        assert_eq!(effective_fps(30, Some(500)), 1);
    }

    #[test]
    fn spa_video_transform_maps_all_variants() {
        // 1:1 ordering with the SPA videotransform values.
        assert_eq!(spa_video_transform(Transform::Normal), 0);
        assert_eq!(spa_video_transform(Transform::_90), 1);
        assert_eq!(spa_video_transform(Transform::_180), 2);
        assert_eq!(spa_video_transform(Transform::_270), 3);
        assert_eq!(spa_video_transform(Transform::Flipped), 4);
        assert_eq!(spa_video_transform(Transform::Flipped90), 5);
        assert_eq!(spa_video_transform(Transform::Flipped180), 6);
        assert_eq!(spa_video_transform(Transform::Flipped270), 7);
    }

    #[test]
    fn clamp_damage_clamps_and_drops() {
        // A rect partly off the right/bottom edge is clamped to the frame.
        assert_eq!(
            clamp_damage(&[(90, 90, 50, 50)], 100, 100, 4),
            vec![(90, 90, 10, 10)]
        );
        // A rect fully outside the frame is dropped, and with nothing left we
        // fall back to the whole frame.
        assert_eq!(
            clamp_damage(&[(200, 0, 10, 10)], 100, 100, 4),
            vec![(0, 0, 100, 100)]
        );
        // A zero-area rect is dropped (then full-frame fallback).
        assert_eq!(
            clamp_damage(&[(0, 0, 0, 10)], 100, 100, 4),
            vec![(0, 0, 100, 100)]
        );
    }

    #[test]
    fn clamp_damage_full_frame_fallbacks() {
        // No damage reported -> whole frame.
        assert_eq!(clamp_damage(&[], 64, 48, 4), vec![(0, 0, 64, 48)]);
        // Zero slots -> whole frame.
        assert_eq!(
            clamp_damage(&[(1, 2, 3, 4)], 64, 48, 0),
            vec![(0, 0, 64, 48)]
        );
        // More rects than slots -> collapse to whole frame rather than truncate.
        let many = [
            (0, 0, 5, 5),
            (5, 5, 5, 5),
            (10, 10, 5, 5),
            (15, 15, 5, 5),
            (20, 20, 5, 5),
        ];
        assert_eq!(clamp_damage(&many, 64, 48, 4), vec![(0, 0, 64, 48)]);
    }

    #[test]
    fn clamp_damage_into_clears_prior_contents() {
        // A reused scratch buffer must not accumulate across frames.
        let mut scratch = vec![(99, 99, 99, 99); 3];
        clamp_damage_into(&mut scratch, &[(0, 0, 10, 10)], 100, 100, 4);
        assert_eq!(scratch, vec![(0, 0, 10, 10)]);
        // Empty input -> full-frame, still no stale entries.
        clamp_damage_into(&mut scratch, &[], 100, 100, 4);
        assert_eq!(scratch, vec![(0, 0, 100, 100)]);
    }

    #[test]
    fn clamp_damage_passes_valid_rects() {
        let rects = [(0, 0, 10, 10), (20, 20, 5, 5)];
        assert_eq!(
            clamp_damage(&rects, 100, 100, 4),
            vec![(0, 0, 10, 10), (20, 20, 5, 5)]
        );
    }

    #[test]
    fn dmabuf_modifier_candidates_orders_and_appends_fallbacks() {
        const INVALID: u64 = 0x00ff_ffff_ffff_ffff;
        const LINEAR: u64 = 0;

        // Explicit modifiers first, then INVALID and LINEAR fallbacks appended.
        assert_eq!(
            dmabuf_modifier_candidates(&[0x1234, 0x5678]),
            vec![0x1234, 0x5678, INVALID, LINEAR]
        );
        // Empty input -> never empty; INVALID then LINEAR.
        assert_eq!(dmabuf_modifier_candidates(&[]), vec![INVALID, LINEAR]);
        // Duplicates and already-present fallbacks are not repeated.
        assert_eq!(
            dmabuf_modifier_candidates(&[LINEAR, LINEAR, INVALID]),
            vec![LINEAR, INVALID]
        );
    }

    #[test]
    fn target_payload_roundtrips() {
        for target in [
            CaptureTarget::Output("DP-1".to_owned()),
            CaptureTarget::Window("firefox@i-3".to_owned()),
            CaptureTarget::Region {
                output: "HDMI-A-1".to_owned(),
                x: 1,
                y: 2,
                width: 3,
                height: 4,
            },
        ] {
            let payload = target.to_payload();
            assert_eq!(CaptureTarget::from_payload(&payload), Some(target));
        }
    }
}
