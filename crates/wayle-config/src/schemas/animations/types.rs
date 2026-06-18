use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
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
    /// Slide in from / out to the top edge with a rotating swing.
    SwingUp,
    /// Slide in from / out to the bottom edge with a rotating swing.
    SwingDown,
    /// Slide in from / out to the left edge with a rotating swing.
    SwingLeft,
    /// Slide in from / out to the right edge with a rotating swing.
    SwingRight,
}

/// Per-surface enter/exit animation override. Any field left unset falls back
/// to the global `[animations]` enter/exit, then to `transition`/`duration`.
#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "kebab-case", default)]
pub struct SurfaceAnimation {
    /// Enter transition for this surface. Unset → global `enter`, then `transition`.
    pub enter: Option<AnimationType>,
    /// Exit transition for this surface. Unset → global `exit`, then `transition`.
    pub exit: Option<AnimationType>,
    /// Enter duration in ms. Unset → global `enter-duration`, then `duration`.
    pub enter_duration: Option<u32>,
    /// Exit duration in ms. Unset → global `exit-duration`, then `duration`.
    pub exit_duration: Option<u32>,
}
