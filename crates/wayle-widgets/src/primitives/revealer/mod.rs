//! Custom revealer driving enter/exit transitions via a per-frame
//! `GskTransform` + opacity, so it can do effects GtkRevealer can't (bounce,
//! genie) on top of the GtkRevealer-equivalent set. Consumes
//! [`AnimationType`] directly; pair with a `gtk::Window` map/unmap like the old
//! revealers (see `shell::helpers::surface_anim`).

mod imp;

use glib::subclass::types::ObjectSubclassIsExt;
use gtk4::{glib, prelude::*};
use wayle_config::schemas::animations::AnimationType;

/// Edge a [`AnimationType::Genie`] collapses toward. The hosting surface feeds
/// this from its own anchor, keeping this crate free of a layer-shell dep.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum GenieEdge {
    /// Suck toward the top edge.
    Top,
    /// Suck toward the bottom edge (default).
    #[default]
    Bottom,
    /// Suck toward the left edge.
    Left,
    /// Suck toward the right edge.
    Right,
}

glib::wrapper! {
    /// Single-child revealer animating its child in/out per the configured
    /// [`AnimationType`]. Drop-in for `gtk::Revealer` in `relm4` `view!` blocks:
    /// `set_transition` / `set_transition_duration` / `set_reveal_child`.
    pub struct WayleRevealer(ObjectSubclass<imp::WayleRevealerImp>)
        @extends gtk4::Widget,
        @implements gtk4::Accessible, gtk4::Buildable, gtk4::ConstraintTarget;
}

impl WayleRevealer {
    /// Creates a new revealer (hidden, fade transition, 200ms).
    #[must_use]
    pub fn new() -> Self {
        glib::Object::builder().build()
    }

    /// Sets the enter/exit transition style.
    pub fn set_transition(&self, transition: AnimationType) {
        self.imp().set_transition(transition);
    }

    /// Sets the full transition duration in milliseconds. `0` snaps instantly.
    pub fn set_transition_duration(&self, ms: u32) {
        self.imp().set_duration(ms);
    }

    /// Edge a genie transition collapses toward.
    pub fn set_genie_edge(&self, edge: GenieEdge) {
        self.imp().set_genie_edge(edge);
    }

    /// Reveals (`true`) or hides (`false`) the child, animating the transition.
    pub fn set_reveal_child(&self, reveal: bool) {
        self.imp().set_reveal_child(reveal);
    }

    /// Whether the child is currently set to be revealed.
    #[must_use]
    pub fn is_child_revealed(&self) -> bool {
        self.imp().reveal_child()
    }

    /// Sets (or clears with `None`) the single child.
    pub fn set_child(&self, child: Option<&impl IsA<gtk4::Widget>>) {
        self.imp()
            .set_child(child.map(|c| c.upcast_ref::<gtk4::Widget>()));
    }

    /// The current child, if any.
    #[must_use]
    pub fn child(&self) -> Option<gtk4::Widget> {
        self.imp().child()
    }
}

impl Default for WayleRevealer {
    fn default() -> Self {
        Self::new()
    }
}

// Let relm4 `view!` nest a child implicitly, exactly like `gtk::Revealer`.
impl relm4::ContainerChild for WayleRevealer {
    type Child = gtk4::Widget;
}

impl relm4::RelmSetChildExt for WayleRevealer {
    fn container_set_child(&self, widget: Option<&impl AsRef<gtk4::Widget>>) {
        self.imp().set_child(widget.map(AsRef::as_ref));
    }

    fn container_get_child(&self) -> Option<gtk4::Widget> {
        self.child()
    }
}
