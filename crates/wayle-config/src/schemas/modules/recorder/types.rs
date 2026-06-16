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
