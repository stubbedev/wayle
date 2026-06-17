//! Share picker surface.
//!
//! A layer-shell overlay that the running shell pops up when the
//! xdg-desktop-portal asks the user to choose a window, output, or region to
//! screen-share. The selection is delivered back to the requesting
//! `wayle share-picker` stub through a oneshot channel carried in the
//! [`SharePickerInput::Show`] message.

mod config;
mod image;
mod util;
mod views;

use config::PickerConfig;
use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{gtk, gtk::prelude::*, prelude::*};
use tokio::sync::oneshot;
use tracing::error;
use wayland_client::Connection;
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
    config: PickerConfig,
    allow_token: bool,
    reply: Option<oneshot::Sender<String>>,
}

#[relm4::component(pub(crate))]
impl Component for SharePicker {
    type Init = ();
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

            gtk::Box {
                set_orientation: gtk::Orientation::Vertical,

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
            }
        }
    }

    fn init(
        _init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let model = SharePicker {
            config: PickerConfig::default(),
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

        widgets
            .token_check
            .connect_toggled(glib_clone_toggle(&sender));

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
                widgets.token_check.set_active(allow_token);

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

                root.set_visible(true);
                root.present();
            }

            SharePickerInput::Select(payload) => {
                if let Some(reply) = self.reply.take() {
                    let prefix = if self.allow_token { "r" } else { "" };
                    let _ = reply.send(format!("{prefix}/{payload}"));
                }
                root.set_visible(false);
            }

            SharePickerInput::ToggleToken(active) => self.allow_token = active,

            SharePickerInput::Cancel => {
                if let Some(reply) = self.reply.take() {
                    let _ = reply.send(String::new());
                }
                root.set_visible(false);
            }
        }
    }
}

/// Builds the `connect_toggled` handler that forwards checkbox state.
fn glib_clone_toggle(
    sender: &ComponentSender<SharePicker>,
) -> impl Fn(&gtk::CheckButton) + 'static {
    let sender = sender.input_sender().clone();
    move |btn| sender.emit(SharePickerInput::ToggleToken(btn.is_active()))
}
