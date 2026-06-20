use wayle_derive::wayle_enum;

/// Container format / codec preset for recordings.
#[wayle_enum(default)]
#[serde(rename_all = "kebab-case")]
pub enum RecorderFormat {
    /// H.264 in an MP4 container.
    Mp4,
    /// H.264 in a Matroska container (resilient to crashes).
    #[default]
    Mkv,
    /// VP9 in a WebM container.
    Webm,
}
