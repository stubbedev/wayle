use serde::{Deserialize, Serialize};
use wayle_derive::wayle_enum;

/// Enter/exit transition style for transient surfaces (OSD, toasts,
/// notifications). Driven per frame by the custom revealer
/// (`GskTransform` + opacity).
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
    /// Scale in from a shrunken state with an elastic overshoot, then settle.
    Bounce,
    /// Suck in toward / out to the surface's anchored edge (macOS "genie"
    /// minimize, approximated with an affine scale-and-slide). Auto-orients to
    /// the edge the surface sits on.
    Genie,
    /// Scale in smoothly from the center with no overshoot.
    Zoom,
    /// Spin in: rotate into place while scaling up and fading in.
    Rotate,
    /// Card flip: open out from a vertical edge (horizontal scale through zero).
    Flip,
}

/// Per-surface enter/exit animation override. Any field left unset falls back
/// to the global `[animations]` enter/exit, then to `transition`/`duration`.
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize)]
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
