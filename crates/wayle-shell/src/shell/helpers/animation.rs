//! Shared animation helpers for transient surfaces.
//!
//! Every animated overlay (OSD, power menu, share picker, notifications,
//! dropdowns, lock screen) drives a [`gtk::Revealer`] whose transition is
//! resolved from the `[animations]` config. The config yields an
//! [`AnimationType`]; this maps it to the GTK revealer transition so each
//! surface doesn't re-implement the same match.

use gtk4::RevealerTransitionType;
use wayle_config::schemas::animations::AnimationType;

/// Maps a configured [`AnimationType`] to its GTK revealer transition.
pub(crate) fn revealer_transition(anim: AnimationType) -> RevealerTransitionType {
    match anim {
        AnimationType::None => RevealerTransitionType::None,
        AnimationType::Fade => RevealerTransitionType::Crossfade,
        AnimationType::SlideUp => RevealerTransitionType::SlideUp,
        AnimationType::SlideDown => RevealerTransitionType::SlideDown,
        AnimationType::SlideLeft => RevealerTransitionType::SlideLeft,
        AnimationType::SlideRight => RevealerTransitionType::SlideRight,
        AnimationType::SwingUp => RevealerTransitionType::SwingUp,
        AnimationType::SwingDown => RevealerTransitionType::SwingDown,
        AnimationType::SwingLeft => RevealerTransitionType::SwingLeft,
        AnimationType::SwingRight => RevealerTransitionType::SwingRight,
    }
}
