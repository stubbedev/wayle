use wayle_derive::wayle_enum;

/// Corner the webcam picture-in-picture frame is anchored to in the recording.
#[wayle_enum(default)]
#[serde(rename_all = "kebab-case")]
pub enum WebcamPosition {
    /// Top-left corner.
    TopLeft,
    /// Top-right corner.
    TopRight,
    /// Bottom-left corner.
    BottomLeft,
    /// Bottom-right corner.
    #[default]
    BottomRight,
}

/// Encoder speed/quality trade-off. Slower presets produce smaller files at
/// the same bitrate, using more CPU.
#[wayle_enum(default)]
#[serde(rename_all = "kebab-case")]
pub enum EncoderPreset {
    /// Fastest, lowest CPU, largest files.
    Speed,
    /// Balanced size and CPU (default).
    #[default]
    Balanced,
    /// Slowest, best compression / smallest files.
    Quality,
}

/// Container format / codec preset for recordings.
#[wayle_enum(default)]
#[serde(rename_all = "kebab-case")]
pub enum RecorderFormat {
    /// H.264 in an MP4 container.
    #[default]
    Mp4,
    /// H.264 in a Matroska container (resilient to crashes).
    Mkv,
    /// VP9 in a WebM container.
    Webm,
}
