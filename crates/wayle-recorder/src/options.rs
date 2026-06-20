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

/// Webcam picture-in-picture overlay options.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WebcamOptions {
    /// V4L2 device path (e.g. `/dev/video0`). Empty auto-selects the first camera.
    pub device: String,
    /// Horizontal position as a percentage of the free horizontal space
    /// (0 = flush left, 100 = flush right). Resolution-independent.
    pub x_percent: u32,
    /// Vertical position as a percentage of the free vertical space
    /// (0 = flush top, 100 = flush bottom). Resolution-independent.
    pub y_percent: u32,
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
    /// Draw the mouse cursor in the recording.
    pub show_cursor: bool,
    /// Audio capture options.
    pub audio: AudioOptions,
    /// Optional webcam picture-in-picture overlay.
    pub webcam: Option<WebcamOptions>,
}
