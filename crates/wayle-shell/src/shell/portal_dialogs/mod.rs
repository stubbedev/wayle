//! Portal dialog host — a custom animated layer-shell surface.
//!
//! Replaces the native `gtk::AlertDialog`/app-chooser with our own overlay so
//! the Access / Account / AppChooser / DynamicLauncher prompts animate
//! congruently through the `[animations]` config (`AnimSurface::Dialog`), like
//! the share picker. Backs `com.wayle.PortalDialogs1`.

use std::sync::Arc;

use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{
    gtk,
    gtk::{gio, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;
use wayle_config::{ConfigService, schemas::animations::AnimSurface};
use wayle_widgets::prelude::WayleRevealer;

use crate::shell::helpers::surface_anim;

/// Messages driving the dialog host.
pub(crate) enum PortalDialogInput {
    /// Generic grant/deny prompt.
    Access {
        title: String,
        subtitle: String,
        body: String,
        grant_label: String,
        deny_label: String,
        reply: oneshot::Sender<bool>,
    },
    /// Consent to share account info.
    Account {
        reason: String,
        reply: oneshot::Sender<bool>,
    },
    /// Pick an application to handle a file/URI.
    ChooseApp {
        choices: Vec<String>,
        content_type: String,
        uri: String,
        reply: oneshot::Sender<String>,
    },
    /// Confirm installing a dynamic launcher.
    ConfirmInstall {
        name: String,
        reply: oneshot::Sender<bool>,
    },
    /// Internal: the user accepted a yes/no prompt.
    Confirm,
    /// Internal: the user dismissed (cancel / Escape).
    Cancel,
    /// Internal: the user picked the app at this list index.
    PickApp(u32),
}

impl std::fmt::Debug for PortalDialogInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Access { .. } => f.write_str("Access"),
            Self::Account { .. } => f.write_str("Account"),
            Self::ChooseApp { .. } => f.write_str("ChooseApp"),
            Self::ConfirmInstall { .. } => f.write_str("ConfirmInstall"),
            Self::Confirm => f.write_str("Confirm"),
            Self::Cancel => f.write_str("Cancel"),
            Self::PickApp(i) => f.debug_tuple("PickApp").field(i).finish(),
        }
    }
}

/// The pending request's reply channel.
enum Pending {
    /// A grant/deny prompt.
    Bool(oneshot::Sender<bool>),
    /// An app chooser; the `Vec` maps list rows to desktop-file ids.
    App(oneshot::Sender<String>, Vec<String>),
}

/// The dialog host component.
pub(crate) struct PortalDialogs {
    config: Arc<ConfigService>,
    pending: Option<Pending>,
}

#[relm4::component(pub(crate))]
impl Component for PortalDialogs {
    type Init = Arc<ConfigService>;
    type Input = PortalDialogInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            add_css_class: "portal-dialog-window",
            set_visible: false,

            #[name = "revealer"]
            WayleRevealer {
                set_reveal_child: false,

                gtk::Box {
                    add_css_class: "portal-dialog-surface",
                    set_orientation: gtk::Orientation::Vertical,
                    set_spacing: 12,
                    set_width_request: 420,

                    #[name = "title_label"]
                    gtk::Label {
                        add_css_class: "portal-dialog-title",
                        set_xalign: 0.0,
                        set_wrap: true,
                    },
                    #[name = "body_label"]
                    gtk::Label {
                        add_css_class: "portal-dialog-body",
                        set_xalign: 0.0,
                        set_wrap: true,
                    },
                    #[name = "app_scroll"]
                    gtk::ScrolledWindow {
                        set_vexpand: true,
                        set_min_content_height: 360,
                        set_visible: false,
                        #[name = "app_list"]
                        gtk::ListBox {
                            add_css_class: "portal-dialog-app-list",
                        },
                    },
                    gtk::Box {
                        set_orientation: gtk::Orientation::Horizontal,
                        set_halign: gtk::Align::End,
                        set_spacing: 8,

                        #[name = "cancel_button"]
                        gtk::Button {
                            add_css_class: "portal-dialog-cancel",
                            connect_clicked => PortalDialogInput::Cancel,
                        },
                        #[name = "confirm_button"]
                        gtk::Button {
                            add_css_class: "portal-dialog-confirm",
                            add_css_class: "suggested-action",
                            connect_clicked => PortalDialogInput::Confirm,
                        },
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
        let model = PortalDialogs {
            config: init,
            pending: None,
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-portal-dialog"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::OnDemand);
        root.set_exclusive_zone(-1);

        add_escape(&root, sender.input_sender().clone());
        widgets.app_list.connect_row_activated({
            let input = sender.input_sender().clone();
            move |_, row| input.emit(PortalDialogInput::PickApp(row.index().max(0) as u32))
        });
        surface_anim::play_on_map(&root, &widgets.revealer);

        ComponentParts { model, widgets }
    }

    fn update_with_view(
        &mut self,
        widgets: &mut Self::Widgets,
        msg: PortalDialogInput,
        _sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            PortalDialogInput::Access {
                title,
                subtitle,
                body,
                grant_label,
                deny_label,
                reply,
            } => {
                let detail = [subtitle, body]
                    .into_iter()
                    .filter(|s| !s.is_empty())
                    .collect::<Vec<_>>()
                    .join("\n");
                self.show_confirm(
                    widgets,
                    root,
                    &title,
                    &detail,
                    &deny_label,
                    &grant_label,
                    reply,
                );
            }
            PortalDialogInput::Account { reason, reply } => {
                let detail = if reason.is_empty() {
                    "An application is requesting your name and avatar.".to_owned()
                } else {
                    reason
                };
                self.show_confirm(
                    widgets,
                    root,
                    "Share account information?",
                    &detail,
                    "Cancel",
                    "Share",
                    reply,
                );
            }
            PortalDialogInput::ConfirmInstall { name, reply } => {
                let title = format!("Install “{name}”?");
                self.show_confirm(widgets, root, &title, "", "Cancel", "Install", reply);
            }
            PortalDialogInput::ChooseApp {
                choices,
                content_type,
                uri,
                reply,
            } => self.show_app_chooser(widgets, root, &choices, &content_type, &uri, reply),

            PortalDialogInput::Confirm => {
                if let Some(Pending::Bool(tx)) = self.pending.take() {
                    let _ = tx.send(true);
                }
                self.dismiss(widgets, root);
            }
            PortalDialogInput::Cancel => {
                match self.pending.take() {
                    Some(Pending::Bool(tx)) => {
                        let _ = tx.send(false);
                    }
                    Some(Pending::App(tx, _)) => {
                        let _ = tx.send(String::new());
                    }
                    None => {}
                }
                self.dismiss(widgets, root);
            }
            PortalDialogInput::PickApp(index) => {
                if let Some(Pending::App(tx, ids)) = self.pending.take() {
                    let id = ids.get(index as usize).cloned().unwrap_or_default();
                    let _ = tx.send(id);
                }
                self.dismiss(widgets, root);
            }
        }
    }
}

impl PortalDialogs {
    /// Cancels any in-flight request before showing a new one.
    fn preempt(&mut self) {
        match self.pending.take() {
            Some(Pending::Bool(tx)) => {
                let _ = tx.send(false);
            }
            Some(Pending::App(tx, _)) => {
                let _ = tx.send(String::new());
            }
            None => {}
        }
    }

    #[allow(clippy::too_many_arguments)]
    fn show_confirm(
        &mut self,
        widgets: &PortalDialogsWidgets,
        root: &gtk::Window,
        title: &str,
        body: &str,
        cancel_label: &str,
        confirm_label: &str,
        reply: oneshot::Sender<bool>,
    ) {
        self.preempt();
        self.pending = Some(Pending::Bool(reply));
        widgets.title_label.set_label(title);
        widgets.body_label.set_label(body);
        widgets.body_label.set_visible(!body.is_empty());
        widgets.app_scroll.set_visible(false);
        widgets.cancel_button.set_label(cancel_label);
        widgets.confirm_button.set_label(confirm_label);
        widgets.confirm_button.set_visible(true);
        surface_anim::reveal(&widgets.revealer, root, &self.config, AnimSurface::Dialog);
    }

    fn show_app_chooser(
        &mut self,
        widgets: &PortalDialogsWidgets,
        root: &gtk::Window,
        choices: &[String],
        content_type: &str,
        _uri: &str,
        reply: oneshot::Sender<String>,
    ) {
        self.preempt();
        let apps = candidate_apps(choices, content_type);
        let ids: Vec<String> = apps
            .iter()
            .map(|app| app.id().map(|id| id.to_string()).unwrap_or_default())
            .collect();

        clear_list(&widgets.app_list);
        for app in &apps {
            let label = gtk::Label::builder()
                .label(app.display_name().as_str())
                .xalign(0.0)
                .margin_top(8)
                .margin_bottom(8)
                .margin_start(12)
                .margin_end(12)
                .build();
            widgets.app_list.append(&label);
        }

        self.pending = Some(Pending::App(reply, ids));
        widgets.title_label.set_label("Open With");
        widgets.body_label.set_visible(false);
        widgets.app_scroll.set_visible(true);
        widgets.cancel_button.set_label("Cancel");
        widgets.confirm_button.set_visible(false);
        surface_anim::reveal(&widgets.revealer, root, &self.config, AnimSurface::Dialog);
    }

    fn dismiss(&self, widgets: &PortalDialogsWidgets, root: &gtk::Window) {
        surface_anim::hide(&widgets.revealer, root, &self.config, AnimSurface::Dialog);
    }
}

/// Removes all rows from a list box.
fn clear_list(list: &gtk::ListBox) {
    while let Some(child) = list.first_child() {
        list.remove(&child);
    }
}

/// Candidate apps: explicit `choices` (desktop ids), else recommended for the
/// content type, else all installed apps.
fn candidate_apps(choices: &[String], content_type: &str) -> Vec<gio::AppInfo> {
    if !choices.is_empty() {
        return gio::AppInfo::all()
            .into_iter()
            .filter(|app| {
                app.id()
                    .map(|id| choices.iter().any(|c| c == id.as_str()))
                    .unwrap_or(false)
            })
            .collect();
    }
    if !content_type.is_empty() {
        let recommended = gio::AppInfo::recommended_for_type(content_type);
        if !recommended.is_empty() {
            return recommended;
        }
    }
    gio::AppInfo::all()
}

/// Adds an Escape-to-cancel controller.
fn add_escape(widget: &impl IsA<gtk::Widget>, input: relm4::Sender<PortalDialogInput>) {
    let controller = gtk::EventControllerKey::new();
    controller.connect_key_pressed(move |_, key, _, _| {
        if key == gtk::gdk::Key::Escape {
            input.emit(PortalDialogInput::Cancel);
            return gtk::glib::Propagation::Stop;
        }
        gtk::glib::Propagation::Proceed
    });
    widget.add_controller(controller);
}
