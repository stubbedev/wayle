use wayle_derive::wayle_enum;

/// Enter/exit transition style for transient surfaces (OSD, toasts,
/// notifications). Maps onto GTK's revealer transitions.
#[wayle_enum(default)]
pub enum AnimationType {
    /// No transition; surfaces appear and disappear instantly.
    None,
    /// Cross-fade opacity in and out.
    #[default]
    Fade,
    /// Slide in from / out to the top edge.
    SlideUp,
    /// Slide in from / out to the bottom edge.
    SlideDown,
    /// Slide in from / out to the left edge.
    SlideLeft,
    /// Slide in from / out to the right edge.
    SlideRight,
}
