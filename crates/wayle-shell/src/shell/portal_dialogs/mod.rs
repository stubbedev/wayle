//! Portal dialog host — a custom animated layer-shell surface.
//!
//! Replaces the native `gtk::AlertDialog`/app-chooser with our own overlay so
//! the Access / Account / AppChooser / DynamicLauncher prompts animate
//! congruently through the `[animations]` config (`AnimSurface::Dialog`), like
//! the share picker. Backs `com.wayle.PortalDialogs1`.
//!
//! The app chooser shows icon + name rows with a live search filter and an
//! "always use for this type" toggle (sets the default handler via GIO). The
//! account prompt previews the local avatar; the dynamic-launcher prompt
//! previews the app icon.

use std::{cell::RefCell, rc::Rc, sync::Arc};

use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{
    gtk,
    gtk::{gio, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;
use tracing::warn;
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
        icon: String,
        reply: oneshot::Sender<bool>,
    },
    /// Consent to share account info.
    Account {
        reason: String,
        reply: oneshot::Sender<bool>,
    },
    /// Preview an image and confirm setting it as the wallpaper.
    WallpaperPreview {
        uri: String,
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
        icon_name: String,
        reply: oneshot::Sender<bool>,
    },
    /// Internal: the user accepted a yes/no prompt.
    Confirm,
    /// Internal: the user dismissed (cancel / Escape).
    Cancel,
    /// Internal: the search filter changed.
    Search(String),
    /// Internal: the user picked the app at this list index.
    PickApp(u32),
}

impl std::fmt::Debug for PortalDialogInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Access { .. } => f.write_str("Access"),
            Self::Account { .. } => f.write_str("Account"),
            Self::WallpaperPreview { .. } => f.write_str("WallpaperPreview"),
            Self::ChooseApp { .. } => f.write_str("ChooseApp"),
            Self::ConfirmInstall { .. } => f.write_str("ConfirmInstall"),
            Self::Confirm => f.write_str("Confirm"),
            Self::Cancel => f.write_str("Cancel"),
            Self::Search(_) => f.write_str("Search"),
            Self::PickApp(i) => f.debug_tuple("PickApp").field(i).finish(),
        }
    }
}

/// The pending request's reply channel.
enum Pending {
    /// A grant/deny prompt.
    Bool(oneshot::Sender<bool>),
    /// An app chooser; the `Vec` maps list rows to desktop-file ids, and
    /// `content_type` is the MIME type to set as default when "always use" is
    /// ticked (empty if the caller gave none).
    App {
        reply: oneshot::Sender<String>,
        ids: Vec<String>,
        content_type: String,
    },
}

/// The dialog host component.
pub(crate) struct PortalDialogs {
    config: Arc<ConfigService>,
    pending: Option<Pending>,
    /// App display names, indexed by list-row position, for the search filter.
    app_names: Rc<RefCell<Vec<String>>>,
    /// Current search query (lower-cased), read by the list filter func.
    query: Rc<RefCell<String>>,
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

                    #[name = "header_image"]
                    gtk::Image {
                        add_css_class: "portal-dialog-image",
                        set_halign: gtk::Align::Center,
                        set_pixel_size: 64,
                        set_visible: false,
                    },
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
                    #[name = "search_entry"]
                    gtk::SearchEntry {
                        add_css_class: "portal-dialog-search",
                        set_visible: false,
                        set_placeholder_text: Some("Search applications…"),
                        connect_search_changed[sender] => move |entry| {
                            sender.input(PortalDialogInput::Search(entry.text().to_string()));
                        },
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
                    #[name = "remember_check"]
                    gtk::CheckButton {
                        add_css_class: "portal-dialog-remember",
                        set_label: Some("Always use for this file type"),
                        set_visible: false,
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
            app_names: Rc::new(RefCell::new(Vec::new())),
            query: Rc::new(RefCell::new(String::new())),
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-portal-dialog"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::OnDemand);
        root.set_exclusive_zone(-1);

        // Hide rows that don't match the query. Rows stay in place (only their
        // visibility flips), so `row.index()` keeps mapping to the same id.
        let filter_names = Rc::clone(&model.app_names);
        let filter_query = Rc::clone(&model.query);
        widgets.app_list.set_filter_func(move |row| {
            let query = filter_query.borrow();
            if query.is_empty() {
                return true;
            }
            let idx = usize::try_from(row.index()).unwrap_or(0);
            filter_names
                .borrow()
                .get(idx)
                .map(|name| name.to_lowercase().contains(query.as_str()))
                .unwrap_or(true)
        });

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
                icon,
                reply,
            } => {
                let detail = [subtitle, body]
                    .into_iter()
                    .filter(|s| !s.is_empty())
                    .collect::<Vec<_>>()
                    .join("\n");
                let header = (!icon.is_empty()).then_some(Header::Icon(icon.as_str()));
                self.show_confirm(
                    widgets,
                    root,
                    &title,
                    &detail,
                    &deny_label,
                    &grant_label,
                    header,
                    64,
                    reply,
                );
            }
            PortalDialogInput::Account { reason, reply } => {
                self.show_account(widgets, root, reason, reply)
            }
            PortalDialogInput::WallpaperPreview { uri, reply } => {
                self.show_wallpaper_preview(widgets, root, uri, reply)
            }
            PortalDialogInput::ConfirmInstall {
                name,
                icon_name,
                reply,
            } => {
                let title = format!("Install “{name}”?");
                let header = (!icon_name.is_empty()).then_some(Header::Icon(icon_name.as_str()));
                self.show_confirm(
                    widgets, root, &title, "", "Cancel", "Install", header, 64, reply,
                );
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
                self.preempt();
                self.dismiss(widgets, root);
            }
            PortalDialogInput::Search(query) => {
                *self.query.borrow_mut() = query.to_lowercase();
                widgets.app_list.invalidate_filter();
            }
            PortalDialogInput::PickApp(index) => {
                if let Some(Pending::App {
                    reply,
                    ids,
                    content_type,
                }) = self.pending.take()
                {
                    let id = ids.get(index as usize).cloned().unwrap_or_default();
                    if widgets.remember_check.is_active() && !content_type.is_empty() {
                        set_default_handler(&id, &content_type);
                    }
                    let _ = reply.send(id);
                }
                self.dismiss(widgets, root);
            }
        }
    }
}

/// What to show in the header image slot.
enum Header<'a> {
    /// A `file://`-less path to load directly (the account avatar).
    File(&'a str),
    /// A themed icon name (the dynamic-launcher app icon).
    Icon(&'a str),
}

impl PortalDialogs {
    /// Cancels any in-flight request before showing a new one.
    fn preempt(&mut self) {
        match self.pending.take() {
            Some(Pending::Bool(tx)) => {
                let _ = tx.send(false);
            }
            Some(Pending::App { reply, .. }) => {
                let _ = reply.send(String::new());
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
        header: Option<Header<'_>>,
        header_px: i32,
        reply: oneshot::Sender<bool>,
    ) {
        self.preempt();
        self.pending = Some(Pending::Bool(reply));
        widgets.header_image.set_pixel_size(header_px);
        set_header(&widgets.header_image, header);
        widgets.title_label.set_label(title);
        widgets.body_label.set_label(body);
        widgets.body_label.set_visible(!body.is_empty());
        widgets.search_entry.set_visible(false);
        widgets.app_scroll.set_visible(false);
        widgets.remember_check.set_visible(false);
        widgets.cancel_button.set_label(cancel_label);
        widgets.confirm_button.set_label(confirm_label);
        widgets.confirm_button.set_visible(true);
        surface_anim::reveal(&widgets.revealer, root, &self.config, AnimSurface::Dialog);
    }

    /// Account-info consent prompt, previewing the local avatar if present.
    fn show_account(
        &mut self,
        widgets: &PortalDialogsWidgets,
        root: &gtk::Window,
        reason: String,
        reply: oneshot::Sender<bool>,
    ) {
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
            local_avatar().as_deref().map(Header::File),
            64,
            reply,
        );
    }

    /// Wallpaper preview confirm, showing the image before it is applied.
    fn show_wallpaper_preview(
        &mut self,
        widgets: &PortalDialogsWidgets,
        root: &gtk::Window,
        uri: String,
        reply: oneshot::Sender<bool>,
    ) {
        let path = uri_to_path(&uri);
        let header = path.as_deref().map(Header::File);
        self.show_confirm(
            widgets,
            root,
            "Set as wallpaper?",
            "",
            "Cancel",
            "Set wallpaper",
            header,
            180,
            reply,
        );
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
        let mut names = Vec::with_capacity(apps.len());
        for app in &apps {
            let name = app.display_name().to_string();
            widgets.app_list.append(&app_row(app, &name));
            names.push(name);
        }
        *self.app_names.borrow_mut() = names;
        *self.query.borrow_mut() = String::new();
        widgets.search_entry.set_text("");
        widgets.app_list.invalidate_filter();

        let can_default = !content_type.is_empty();
        self.pending = Some(Pending::App {
            reply,
            ids,
            content_type: content_type.to_owned(),
        });
        set_header(&widgets.header_image, None);
        widgets.title_label.set_label("Open With");
        widgets.body_label.set_visible(false);
        widgets.search_entry.set_visible(true);
        widgets.app_scroll.set_visible(true);
        widgets.remember_check.set_active(false);
        widgets.remember_check.set_visible(can_default);
        widgets.cancel_button.set_label("Cancel");
        widgets.confirm_button.set_visible(false);
        surface_anim::reveal(&widgets.revealer, root, &self.config, AnimSurface::Dialog);
    }

    fn dismiss(&self, widgets: &PortalDialogsWidgets, root: &gtk::Window) {
        surface_anim::hide(&widgets.revealer, root, &self.config, AnimSurface::Dialog);
    }
}

/// Sets (or hides) the header image slot.
fn set_header(image: &gtk::Image, header: Option<Header<'_>>) {
    match header {
        Some(Header::File(path)) => {
            image.set_from_file(Some(path));
            image.set_visible(true);
        }
        Some(Header::Icon(name)) => {
            image.set_icon_name(Some(name));
            image.set_visible(true);
        }
        None => image.set_visible(false),
    }
}

/// Builds an icon + name row for the app list.
fn app_row(app: &gio::AppInfo, name: &str) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(10)
        .margin_top(6)
        .margin_bottom(6)
        .margin_start(10)
        .margin_end(10)
        .build();

    let icon = gtk::Image::builder().pixel_size(32).build();
    if let Some(gicon) = app.icon() {
        icon.set_from_gicon(&gicon);
    } else {
        icon.set_icon_name(Some("application-x-executable-symbolic"));
    }
    icon.add_css_class("portal-dialog-app-icon");
    row.append(&icon);

    let text = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .valign(gtk::Align::Center)
        .build();
    text.append(
        &gtk::Label::builder()
            .label(name)
            .xalign(0.0)
            .css_classes(["portal-dialog-app-name"])
            .build(),
    );
    if let Some(desc) = app.description().filter(|d| !d.is_empty()) {
        text.append(
            &gtk::Label::builder()
                .label(desc.as_str())
                .xalign(0.0)
                .ellipsize(gtk::pango::EllipsizeMode::End)
                .css_classes(["portal-dialog-app-desc"])
                .build(),
        );
    }
    row.append(&text);
    row
}

/// Sets `id` as the default handler for `content_type`. Best-effort: logs and
/// moves on if the entry can't be resolved or the association write fails.
fn set_default_handler(id: &str, content_type: &str) {
    let Some(app) = gio::AppInfo::all()
        .into_iter()
        .find(|app| app.id().map(|i| i.as_str() == id).unwrap_or(false))
    else {
        warn!(%id, "appchooser: cannot resolve app for set-default");
        return;
    };
    if let Err(err) = app.set_as_default_for_type(content_type) {
        warn!(%id, %content_type, %err, "appchooser: set-default failed");
    }
}

/// Decodes a `file://` URI into a filesystem path (undoing `%XX` escapes), for
/// loading the wallpaper preview image. Returns `None` for non-`file` schemes.
fn uri_to_path(uri: &str) -> Option<String> {
    let rest = uri.strip_prefix("file://")?;
    let path = match rest.find('/') {
        Some(0) => rest,
        Some(slash) => &rest[slash..],
        None => return None,
    };
    let bytes = path.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%'
            && i + 2 < bytes.len()
            && let Ok(byte) = u8::from_str_radix(&path[i + 1..i + 3], 16)
        {
            out.push(byte);
            i += 3;
            continue;
        }
        out.push(bytes[i]);
        i += 1;
    }
    Some(String::from_utf8_lossy(&out).into_owned())
}

/// Reads the local avatar path (`~/.face`) if present, for the account prompt.
fn local_avatar() -> Option<String> {
    let path = std::env::var_os("HOME").map(std::path::PathBuf::from)?.join(".face");
    path.exists().then(|| path.to_string_lossy().into_owned())
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
