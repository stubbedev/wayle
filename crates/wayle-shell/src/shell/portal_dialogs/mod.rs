//! Portal dialog host.
//!
//! Native GTK dialogs for the portal backend's Access / Account / AppChooser /
//! DynamicLauncher interfaces, on behalf of `com.wayle.PortalDialogs1`. Runs on
//! the GTK thread (dialogs need it). Uses GTK's own `AlertDialog` for the
//! yes/no prompts and a small `ListBox` window for the app chooser — no
//! xdg-desktop-portal-gtk.

use relm4::{
    gtk,
    gtk::{gio, glib, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;

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
}

impl std::fmt::Debug for PortalDialogInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Access { title, .. } => f
                .debug_struct("Access")
                .field("title", title)
                .finish_non_exhaustive(),
            Self::Account { .. } => f.write_str("Account"),
            Self::ChooseApp { uri, .. } => f
                .debug_struct("ChooseApp")
                .field("uri", uri)
                .finish_non_exhaustive(),
            Self::ConfirmInstall { name, .. } => f
                .debug_struct("ConfirmInstall")
                .field("name", name)
                .finish_non_exhaustive(),
        }
    }
}

/// The dialog host component.
pub(crate) struct PortalDialogs;

#[relm4::component(pub(crate))]
impl Component for PortalDialogs {
    type Init = ();
    type Input = PortalDialogInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            set_visible: false,
        }
    }

    fn init(
        _init: Self::Init,
        root: Self::Root,
        _sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let widgets = view_output!();
        ComponentParts {
            model: PortalDialogs,
            widgets,
        }
    }

    fn update(
        &mut self,
        msg: PortalDialogInput,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
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
                confirm(&title, &detail, &deny_label, &grant_label, reply);
            }
            PortalDialogInput::Account { reason, reply } => {
                let detail = if reason.is_empty() {
                    "An application is requesting your name and avatar.".to_owned()
                } else {
                    reason
                };
                confirm(
                    "Share account information?",
                    &detail,
                    "Cancel",
                    "Share",
                    reply,
                );
            }
            PortalDialogInput::ConfirmInstall { name, reply } => {
                confirm(
                    &format!("Install “{name}”?"),
                    "",
                    "Cancel",
                    "Install",
                    reply,
                );
            }
            PortalDialogInput::ChooseApp {
                choices,
                content_type,
                uri,
                reply,
            } => choose_app(&choices, &content_type, &uri, reply),
        }
    }
}

/// Shows a two-button `AlertDialog`; replies `true` if the second (grant)
/// button is chosen.
fn confirm(message: &str, detail: &str, deny: &str, grant: &str, reply: oneshot::Sender<bool>) {
    let dialog = gtk::AlertDialog::builder()
        .modal(true)
        .message(message)
        .detail(detail)
        .buttons([deny, grant])
        .cancel_button(0)
        .default_button(1)
        .build();
    dialog.choose(gtk::Window::NONE, gio::Cancellable::NONE, move |result| {
        let granted = matches!(result, Ok(1));
        let _ = reply.send(granted);
    });
}

/// Shows an app picker; replies with the chosen desktop-file id (empty on
/// cancel).
fn choose_app(choices: &[String], content_type: &str, _uri: &str, reply: oneshot::Sender<String>) {
    let apps = candidate_apps(choices, content_type);

    let window = gtk::Window::builder()
        .title("Open With")
        .modal(true)
        .default_width(420)
        .default_height(520)
        .build();
    let list = gtk::ListBox::builder()
        .css_classes(["portal-app-list"])
        .build();
    for app in &apps {
        let row = gtk::Label::builder()
            .label(app.display_name().as_str())
            .xalign(0.0)
            .margin_top(8)
            .margin_bottom(8)
            .margin_start(12)
            .margin_end(12)
            .build();
        list.append(&row);
    }
    let scrolled = gtk::ScrolledWindow::builder()
        .vexpand(true)
        .child(&list)
        .build();
    let container = gtk::Box::new(gtk::Orientation::Vertical, 0);
    container.append(&scrolled);
    window.set_child(Some(&container));

    // One shared slot: whichever of row-activate / close fires first takes the
    // reply (chosen id, or empty on cancel).
    let reply_cell = std::rc::Rc::new(std::cell::RefCell::new(Some(reply)));
    let ids: std::rc::Rc<Vec<String>> = std::rc::Rc::new(
        apps.iter()
            .map(|app| app.id().map(|id| id.to_string()).unwrap_or_default())
            .collect(),
    );

    let win = window.clone();
    let activate_cell = reply_cell.clone();
    list.connect_row_activated(move |_, row| {
        let id = ids.get(row.index() as usize).cloned().unwrap_or_default();
        if let Some(reply) = activate_cell.borrow_mut().take() {
            let _ = reply.send(id);
        }
        win.close();
    });

    window.connect_close_request(move |_| {
        if let Some(reply) = reply_cell.borrow_mut().take() {
            let _ = reply.send(String::new());
        }
        glib::Propagation::Proceed
    });

    window.present();
}

/// Candidate apps: the explicit `choices` (desktop ids) if given, else apps
/// recommended for `content_type`, else all installed apps.
fn candidate_apps(choices: &[String], content_type: &str) -> Vec<gio::AppInfo> {
    if !choices.is_empty() {
        let all = gio::AppInfo::all();
        return all
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
