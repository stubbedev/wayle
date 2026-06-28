//! Print — a custom animated layer-shell surface.
//!
//! Replaces `GtkPrintUnixDialog` with our own printer picker so the portal print
//! prompt animates congruently (`AnimSurface::Print`). `Prepare` shows the
//! picker and stashes the chosen printer under a token; `Spool` re-resolves that
//! printer and sends the document fd via `GtkPrintJob`. Backs `com.wayle.Print1`.

use std::{
    collections::HashMap,
    os::fd::{IntoRawFd, OwnedFd},
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
};

use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{gtk, gtk::prelude::*, prelude::*};
use tokio::sync::oneshot;
use tracing::warn;
use wayle_config::{ConfigService, schemas::animations::AnimSurface};
use wayle_widgets::prelude::WayleRevealer;

use crate::shell::helpers::surface_anim;

/// Flat GTK print-setting key/value pairs (we use printer defaults, so empty).
pub(crate) type SettingsPairs = Vec<(String, String)>;
/// Reply for a prepare request: `Some((settings, token))` or `None` on cancel.
type PrepareReply = oneshot::Sender<Option<(SettingsPairs, u32)>>;

/// Messages driving the print host.
pub(crate) enum PrintInput {
    /// Show the printer picker; reply with `(settings, token)` or `None`.
    Prepare { title: String, reply: PrepareReply },
    /// Spool `document` to the printer prepared under `token`.
    Spool {
        title: String,
        document: OwnedFd,
        token: u32,
        reply: oneshot::Sender<bool>,
    },
    /// Internal: the user confirmed the selected printer.
    Confirm,
    /// Internal: cancel.
    Cancel,
}

impl std::fmt::Debug for PrintInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Prepare { title, .. } => f
                .debug_struct("Prepare")
                .field("title", title)
                .finish_non_exhaustive(),
            Self::Spool { token, .. } => f
                .debug_struct("Spool")
                .field("token", token)
                .finish_non_exhaustive(),
            Self::Confirm => f.write_str("Confirm"),
            Self::Cancel => f.write_str("Cancel"),
        }
    }
}

/// The print host component.
pub(crate) struct Print {
    config: Arc<ConfigService>,
    printers: Vec<String>,
    tokens: HashMap<u32, String>,
    next_token: u32,
    pending: Option<PrepareReply>,
}

#[relm4::component(pub(crate))]
impl Component for Print {
    type Init = Arc<ConfigService>;
    type Input = PrintInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            add_css_class: "print-window",
            set_visible: false,

            #[name = "revealer"]
            WayleRevealer {
                set_reveal_child: false,

                gtk::Box {
                    add_css_class: "print-surface",
                    set_orientation: gtk::Orientation::Vertical,
                    set_spacing: 12,
                    set_width_request: 460,

                    gtk::Label {
                        add_css_class: "print-title",
                        set_xalign: 0.0,
                        set_label: "Print",
                    },
                    gtk::ScrolledWindow {
                        set_vexpand: true,
                        set_min_content_height: 320,
                        #[name = "printer_list"]
                        gtk::ListBox {
                            add_css_class: "print-printer-list",
                            set_selection_mode: gtk::SelectionMode::Single,
                        },
                    },
                    gtk::Box {
                        set_orientation: gtk::Orientation::Horizontal,
                        set_halign: gtk::Align::End,
                        set_spacing: 8,
                        #[name = "cancel_button"]
                        gtk::Button {
                            set_label: "Cancel",
                            connect_clicked => PrintInput::Cancel,
                        },
                        #[name = "confirm_button"]
                        gtk::Button {
                            set_label: "Print",
                            add_css_class: "suggested-action",
                            connect_clicked => PrintInput::Confirm,
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
        let model = Print {
            config: init,
            printers: Vec::new(),
            tokens: HashMap::new(),
            next_token: 1,
            pending: None,
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-print"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::OnDemand);
        root.set_exclusive_zone(-1);
        surface_anim::play_on_map(&root, &widgets.revealer);

        ComponentParts { model, widgets }
    }

    fn update_with_view(
        &mut self,
        widgets: &mut Self::Widgets,
        msg: PrintInput,
        _sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            PrintInput::Prepare { title: _, reply } => {
                if let Some(prev) = self.pending.take() {
                    let _ = prev.send(None);
                }
                self.pending = Some(reply);
                self.printers = enumerate_printer_names();

                clear_list(&widgets.printer_list);
                for name in &self.printers {
                    let label = gtk::Label::builder()
                        .label(name)
                        .xalign(0.0)
                        .margin_top(6)
                        .margin_bottom(6)
                        .margin_start(10)
                        .margin_end(10)
                        .build();
                    widgets.printer_list.append(&label);
                }
                if let Some(first) = widgets.printer_list.row_at_index(0) {
                    widgets.printer_list.select_row(Some(&first));
                }
                surface_anim::reveal(&widgets.revealer, root, &self.config, AnimSurface::Print);
            }
            PrintInput::Confirm => {
                let selected = widgets
                    .printer_list
                    .selected_row()
                    .and_then(|row| usize::try_from(row.index()).ok())
                    .and_then(|i| self.printers.get(i).cloned());
                match (self.pending.take(), selected) {
                    (Some(reply), Some(printer)) => {
                        let token = self.next_token;
                        self.next_token = self.next_token.wrapping_add(1).max(1);
                        self.tokens.insert(token, printer);
                        let _ = reply.send(Some((Vec::new(), token)));
                    }
                    (Some(reply), None) => {
                        let _ = reply.send(None);
                    }
                    _ => {}
                }
                surface_anim::hide(&widgets.revealer, root, &self.config, AnimSurface::Print);
            }
            PrintInput::Cancel => {
                if let Some(reply) = self.pending.take() {
                    let _ = reply.send(None);
                }
                surface_anim::hide(&widgets.revealer, root, &self.config, AnimSurface::Print);
            }
            PrintInput::Spool {
                title,
                document,
                token,
                reply,
            } => {
                let printer = self.tokens.remove(&token);
                let _ = reply.send(match printer {
                    Some(name) => spool(&title, document, &name),
                    None => {
                        warn!(token, "print: no prepared printer for token");
                        false
                    }
                });
            }
        }
    }
}

/// Enumerates available printer names (synchronous).
fn enumerate_printer_names() -> Vec<String> {
    let names = Arc::new(Mutex::new(Vec::new()));
    let collector = Arc::clone(&names);
    gtk::enumerate_printers(
        move |printer| {
            if let Ok(mut names) = collector.lock() {
                names.push(printer.name().to_string());
            }
            true
        },
        true,
    );
    Arc::try_unwrap(names)
        .ok()
        .and_then(|m| m.into_inner().ok())
        .unwrap_or_default()
}

/// Spools `document` to the named printer with default settings via a
/// `GtkPrintJob`. Returns whether a matching printer was found and queued.
fn spool(title: &str, document: OwnedFd, printer_name: &str) -> bool {
    // The print job reads the fd asynchronously while spooling, so hand it the
    // raw fd and let the job own it (leaked from our side intentionally).
    let raw = document.into_raw_fd();
    let title = title.to_owned();
    let target = printer_name.to_owned();
    let sent = Arc::new(AtomicBool::new(false));
    let sent_cb = Arc::clone(&sent);

    gtk::enumerate_printers(
        move |printer| {
            if printer.name() != target.as_str() {
                return true;
            }
            let job = gtk::PrintJob::new(
                &title,
                printer,
                &gtk::PrintSettings::new(),
                &gtk::PageSetup::new(),
            );
            if job.set_source_fd(raw).is_ok() {
                job.send(|_, _| {});
                sent_cb.store(true, Ordering::SeqCst);
            }
            false
        },
        true,
    );
    sent.load(Ordering::SeqCst)
}

/// Removes all rows from a list box.
fn clear_list(list: &gtk::ListBox) {
    while let Some(child) = list.first_child() {
        list.remove(&child);
    }
}
