//! Share picker surface.
//!
//! A layer-shell overlay that the running shell pops up when the
//! xdg-desktop-portal asks the user to choose a window, output, or region to
//! screen-share. The selection is delivered back to the requesting
//! `wayle share-picker` stub through a oneshot channel carried in the
//! [`SharePickerInput::Show`] message.
//!
//! Enter/exit are animated through a [`gtk::Revealer`] using the same
//! `[animations]` surface model as toasts, the OSD, and notification popups
//! (see [`AnimSurface::SharePicker`]).

mod config;
mod image;
mod util;
mod views;

use std::{sync::Arc, time::Duration};

use config::PickerConfig;
use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{gtk, gtk::prelude::*, prelude::*};
use tokio::sync::oneshot;
use tracing::error;
use wayland_client::Connection;
use wayle_config::{
    ConfigService,
    schemas::animations::{AnimSurface, AnimationType},
};
use wayle_share_preview::toplevel::Toplevel;

/// Messages driving the picker.
pub(crate) enum SharePickerInput {
    /// Open the picker for a fresh portal request. `reply` receives the XDPH
    /// selection suffix (`r/window:123`, `/screen:DP-1`, ...) or an empty
    /// string when the user cancels.
    Show {
        /// Parsed `XDPH_WINDOW_SHARING_LIST` entries.
        toplevels: Vec<Toplevel>,
        /// Initial state of the restore-token checkbox.
        allow_token: bool,
        /// Channel the chosen selection is sent back on.
        reply: oneshot::Sender<String>,
    },
    /// A card/region was chosen; payload is `window:<id>`/`screen:<name>`/
    /// `region:<spec>`.
    Select(String),
    /// Restore-token checkbox toggled.
    ToggleToken(bool),
    /// Picker dismissed without a selection.
    Cancel,
}

impl std::fmt::Debug for SharePickerInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Show {
                toplevels,
                allow_token,
                ..
            } => f
                .debug_struct("Show")
                .field("toplevels", &toplevels.len())
                .field("allow_token", allow_token)
                .finish_non_exhaustive(),
            Self::Select(payload) => f.debug_tuple("Select").field(payload).finish(),
            Self::ToggleToken(active) => f.debug_tuple("ToggleToken").field(active).finish(),
            Self::Cancel => f.write_str("Cancel"),
        }
    }
}

/// The share picker component.
pub(crate) struct SharePicker {
    config_service: Arc<ConfigService>,
    /// Snapshot of `[share-picker]`, refreshed each time the picker opens.
    config: PickerConfig,
    allow_token: bool,
    reply: Option<oneshot::Sender<String>>,
}

#[relm4::component(pub(crate))]
impl Component for SharePicker {
    type Init = Arc<ConfigService>;
    type Input = SharePickerInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            add_css_class: "share-picker-window",
            set_default_size: (model.config.width, model.config.height),
            set_visible: false,

            #[name = "revealer"]
            gtk::Revealer {
                set_reveal_child: false,

                // The surface carries the background and an explicit size, so
                // the transparent window wraps it tightly (no see-through
                // margin) and the revealer animates the whole panel as one —
                // exactly like a notification card.
                #[name = "surface"]
                gtk::Box {
                    add_css_class: "share-picker-surface",
                    set_orientation: gtk::Orientation::Vertical,
                    set_size_request: (model.config.width, model.config.height),

                    #[name = "notebook"]
                    gtk::Notebook {
                        set_vexpand: true,
                        add_css_class: "share-picker-notebook",
                    },

                    #[name = "token_check"]
                    gtk::CheckButton {
                        set_label: Some("Allow a restore token"),
                        add_css_class: "share-picker-restore-button",
                        set_visible: !model.config.hide_token_restore,
                    },
                },
            }
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let config = PickerConfig::from_config(init.config());
        let model = SharePicker {
            config_service: init,
            config,
            allow_token: false,
            reply: None,
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-share-picker"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::OnDemand);
        root.set_exclusive_zone(-1);

        views::add_escape_controller(&root, sender.input_sender().clone());

        widgets.token_check.set_cursor_from_name(Some("pointer"));
        widgets
            .token_check
            .connect_toggled(glib_clone_toggle(&sender));

        // Play the enter transition only once the freshly-mapped window is on
        // screen. Flipping `reveal_child` before the map (e.g. on an idle right
        // after `set_visible`) makes GTK treat the revealed state as initial
        // and skip the animation — which is why only the exit was animating.
        // The window unmaps on hide, so `map` fires again for every open.
        let revealer = widgets.revealer.clone();
        root.connect_map(move |_| {
            let revealer = revealer.clone();
            gtk::glib::idle_add_local_once(move || revealer.set_reveal_child(true));
        });

        ComponentParts { model, widgets }
    }

    fn update_with_view(
        &mut self,
        widgets: &mut Self::Widgets,
        msg: SharePickerInput,
        sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            SharePickerInput::Show {
                toplevels,
                allow_token,
                reply,
            } => {
                // Drop any previous, unanswered request.
                if let Some(prev) = self.reply.take() {
                    let _ = prev.send(String::new());
                }
                self.allow_token = allow_token;
                self.reply = Some(reply);

                // Re-resolve config so live settings edits apply per request.
                self.config = PickerConfig::from_config(&self.config_service.config());
                widgets
                    .surface
                    .set_size_request(self.config.width, self.config.height);
                widgets.token_check.set_active(allow_token);
                widgets
                    .token_check
                    .set_visible(!self.config.hide_token_restore);

                while widgets.notebook.n_pages() > 0 {
                    widgets.notebook.remove_page(Some(0));
                }
                match Connection::connect_to_env() {
                    Ok(con) => views::populate_notebook(
                        &widgets.notebook,
                        &con,
                        &toplevels,
                        &self.config,
                        sender.input_sender(),
                    ),
                    Err(err) => error!(%err, "share picker: cannot connect to wayland"),
                }

                self.reveal(widgets, root);
            }

            SharePickerInput::Select(payload) => {
                if let Some(reply) = self.reply.take() {
                    let prefix = if self.allow_token { "r" } else { "" };
                    let _ = reply.send(format!("{prefix}/{payload}"));
                }
                self.hide_animated(widgets, root);
            }

            SharePickerInput::ToggleToken(active) => self.allow_token = active,

            SharePickerInput::Cancel => {
                if let Some(reply) = self.reply.take() {
                    let _ = reply.send(String::new());
                }
                self.hide_animated(widgets, root);
            }
        }
    }
}

impl SharePicker {
    /// Resolved revealer transition + duration for the share-picker surface.
    fn animation(&self, exiting: bool) -> (gtk::RevealerTransitionType, u32) {
        let animations = &self.config_service.config().animations;
        let transition = revealer_transition(
            animations.transition_for(AnimSurface::SharePicker, exiting),
        );
        (
            transition,
            animations.duration_for(AnimSurface::SharePicker, exiting),
        )
    }

    /// Arms the enter transition from the collapsed state, then maps the
    /// window. The actual reveal is flipped by the window's `map` handler
    /// (wired in `init`) so the transition plays after the window is on screen.
    fn reveal(&self, widgets: &SharePickerWidgets, root: &gtk::Window) {
        let (transition, duration) = self.animation(false);
        widgets.revealer.set_transition_type(transition);
        widgets.revealer.set_transition_duration(duration);
        widgets.revealer.set_reveal_child(false);

        root.set_visible(true);
        root.present();
    }

    /// Plays the exit transition, then unmaps the window once it finishes.
    fn hide_animated(&self, widgets: &SharePickerWidgets, root: &gtk::Window) {
        let (transition, duration) = self.animation(true);
        widgets.revealer.set_transition_type(transition);
        widgets.revealer.set_transition_duration(duration);
        widgets.revealer.set_reveal_child(false);

        let root = root.clone();
        gtk::glib::timeout_add_local_once(Duration::from_millis(u64::from(duration)), move || {
            root.set_visible(false);
        });
    }
}

/// Maps an [`AnimationType`] onto its GTK revealer transition.
fn revealer_transition(anim: AnimationType) -> gtk::RevealerTransitionType {
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

/// Builds the `connect_toggled` handler that forwards checkbox state.
fn glib_clone_toggle(
    sender: &ComponentSender<SharePicker>,
) -> impl Fn(&gtk::CheckButton) + 'static {
    let sender = sender.input_sender().clone();
    move |btn| sender.emit(SharePickerInput::ToggleToken(btn.is_active()))
}
