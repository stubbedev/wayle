//! ScreenCast source types and picker-selection parsing (pure logic).

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
