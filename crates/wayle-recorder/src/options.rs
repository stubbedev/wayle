//! Recording options consumed by the GStreamer pipeline builder.

/// Container format / codec preset for a recording.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OutputFormat {
    /// H.264 video + Opus audio in an MP4 container.
    Mp4,
    /// H.264 video + Opus audio in a Matroska container.
    Mkv,
    /// VP9 video + Opus audio in a WebM container.
    Webm,
}

impl OutputFormat {
    /// File extension (without the dot) for this format.
    #[must_use]
    pub fn extension(self) -> &'static str {
        match self {
            Self::Mp4 => "mp4",
            Self::Mkv => "mkv",
            Self::Webm => "webm",
        }
    }
}

/// Encoder speed/quality trade-off.
///
/// Slower presets compress better (smaller files at the same bitrate) at the
/// cost of more CPU.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EncoderPreset {
    /// Fastest, lowest CPU, largest files.
    Speed,
    /// Sensible default balance of size and CPU.
    Balanced,
    /// Slowest, best compression / smallest files.
    Quality,
}

impl EncoderPreset {
    /// `x264enc speed-preset` value.
    pub(crate) fn x264(self) -> &'static str {
        match self {
            Self::Speed => "veryfast",
            Self::Balanced => "medium",
            Self::Quality => "slow",
        }
    }

    /// `vp9enc cpu-used` value (lower is slower / better).
    pub(crate) fn vp9_cpu_used(self) -> u32 {
        match self {
            Self::Speed => 5,
            Self::Balanced => 2,
            Self::Quality => 1,
        }
    }
}

/// Corner the webcam picture-in-picture frame is anchored to.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WebcamPosition {
    /// Top-left corner.
    TopLeft,
    /// Top-right corner.
    TopRight,
    /// Bottom-left corner.
    BottomLeft,
    /// Bottom-right corner.
    BottomRight,
}

/// Webcam picture-in-picture overlay options.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WebcamOptions {
    /// V4L2 device path (e.g. `/dev/video0`). Empty auto-selects the first camera.
    pub device: String,
    /// Corner the frame is anchored to.
    pub position: WebcamPosition,
    /// Frame width as a percentage of the recording width (1-100).
    pub size_percent: u32,
}

/// Audio capture options.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AudioOptions {
    /// Capture the microphone.
    pub microphone: bool,
    /// Microphone PipeWire/PulseAudio source name. Empty uses the default source.
    pub microphone_device: String,
    /// Capture desktop (system) audio via the default sink monitor.
    pub system_audio: bool,
    /// Opus audio bitrate per track, in kilobits per second.
    pub bitrate_kbps: u32,
    /// Keep microphone and system audio as separate tracks (editable) rather
    /// than mixing them into one.
    pub separate_tracks: bool,
}

/// Full set of options for a recording session.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordOptions {
    /// Absolute path of the output file.
    pub output_path: String,
    /// Container format / codec preset.
    pub format: OutputFormat,
    /// Capture framerate in frames per second.
    pub framerate: u32,
    /// Video bitrate in kilobits per second.
    pub bitrate_kbps: u32,
    /// Encoder speed/quality trade-off.
    pub preset: EncoderPreset,
    /// Draw the mouse cursor in the recording.
    pub show_cursor: bool,
    /// Audio capture options.
    pub audio: AudioOptions,
    /// Optional webcam picture-in-picture overlay.
    pub webcam: Option<WebcamOptions>,
}
