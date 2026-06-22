//! Shared enter/exit animation scaffolding for transient layer-shell surfaces.
//!
//! Mirrors the share-picker/OSD pattern (a `gtk::Revealer` wrapping the surface
//! content, flipped open on map and closed on dismiss) so every portal surface
//! animates congruently through the `[animations]` config — honoring the
//! per-surface → global → base `transition`/`duration` inheritance cascade.

use std::{sync::Arc, time::Duration};

use relm4::gtk::{self, prelude::*};
use wayle_config::{
    ConfigService,
    schemas::animations::{AnimSurface, AnimationType},
};

/// Maps an [`AnimationType`] onto its GTK revealer transition.
pub(crate) fn revealer_transition(anim: AnimationType) -> gtk::RevealerTransitionType {
    match anim {
        AnimationType::None => gtk::RevealerTransitionType::None,
        AnimationType::Fade => gtk::RevealerTransitionType::Crossfade,
        AnimationType::SlideUp => gtk::RevealerTransitionType::SlideUp,
        AnimationType::SlideDown => gtk::RevealerTransitionType::SlideDown,
        AnimationType::SlideLeft => gtk::RevealerTransitionType::SlideLeft,
        AnimationType::SlideRight => gtk::RevealerTransitionType::SlideRight,
        AnimationType::SwingUp => gtk::RevealerTransitionType::SwingUp,
        AnimationType::SwingDown => gtk::RevealerTransitionType::SwingDown,
        AnimationType::SwingLeft => gtk::RevealerTransitionType::SwingLeft,
        AnimationType::SwingRight => gtk::RevealerTransitionType::SwingRight,
    }
}

/// Resolved (transition, duration_ms) for a surface and direction.
fn animation(
    config: &Arc<ConfigService>,
    surface: AnimSurface,
    exiting: bool,
) -> (gtk::RevealerTransitionType, u32) {
    let animations = &config.config().animations;
    (
        revealer_transition(animations.transition_for(surface, exiting)),
        animations.duration_for(surface, exiting),
    )
}

/// Arms the enter transition from the collapsed state and maps the window. The
/// reveal is flipped by the window's `map` handler ([`play_on_map`]) so the
/// transition plays after the surface is on screen.
pub(crate) fn reveal(
    revealer: &gtk::Revealer,
    root: &gtk::Window,
    config: &Arc<ConfigService>,
    surface: AnimSurface,
) {
    let (transition, duration) = animation(config, surface, false);
    revealer.set_transition_type(transition);
    revealer.set_transition_duration(duration);
    revealer.set_reveal_child(false);
    root.set_visible(true);
    root.present();

    // If the window is already mapped (e.g. a second request arrives before the
    // previous one finished hiding), no fresh `map` fires, so `play_on_map`
    // won't re-open the revealer — drive it directly.
    if root.is_mapped() {
        let revealer = revealer.clone();
        gtk::glib::idle_add_local_once(move || revealer.set_reveal_child(true));
    }
}

/// Plays the exit transition, then unmaps the window once it finishes.
pub(crate) fn hide(
    revealer: &gtk::Revealer,
    root: &gtk::Window,
    config: &Arc<ConfigService>,
    surface: AnimSurface,
) {
    let (transition, duration) = animation(config, surface, true);
    revealer.set_transition_type(transition);
    revealer.set_transition_duration(duration);
    revealer.set_reveal_child(false);

    let root = root.clone();
    gtk::glib::timeout_add_local_once(Duration::from_millis(u64::from(duration)), move || {
        root.set_visible(false);
    });
}

/// Wires the window's `map` so the revealer opens once mapped, playing the enter
/// transition. (Flipping `reveal_child` before map makes GTK skip the
/// animation.) Call once from the component's `init`.
pub(crate) fn play_on_map(root: &gtk::Window, revealer: &gtk::Revealer) {
    let revealer = revealer.clone();
    root.connect_map(move |_| {
        let revealer = revealer.clone();
        gtk::glib::idle_add_local_once(move || revealer.set_reveal_child(true));
    });
}
