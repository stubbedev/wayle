//! Print host.
//!
//! Native printing for the portal backend's `org.freedesktop.impl.portal.Print`
//! via GTK's own `PrintUnixDialog` + `PrintJob` — no xdg-desktop-portal-gtk.
//! `Prepare` shows the dialog and stashes the chosen printer/settings under a
//! token; `Spool` sends the document fd to that printer.

use std::{cell::RefCell, collections::HashMap, os::fd::AsRawFd, rc::Rc};

use relm4::{gtk, gtk::prelude::*, prelude::*};
use tokio::sync::oneshot;
use tracing::warn;

/// A prepared print target.
#[derive(Clone)]
struct Prepared {
    printer: gtk::Printer,
    settings: gtk::PrintSettings,
    page_setup: gtk::PageSetup,
}

/// Flat GTK print-setting key/value pairs.
pub(crate) type SettingsPairs = Vec<(String, String)>;
/// Reply for a prepare request: `Some((settings, token))` or `None` on cancel.
type PrepareReply = oneshot::Sender<Option<(SettingsPairs, u32)>>;

/// Messages driving the print host.
pub(crate) enum PrintInput {
    /// Show the print dialog; reply `Some((settings, token))` or `None` on cancel.
    Prepare { title: String, reply: PrepareReply },
    /// Spool `document` to the printer prepared under `token`.
    Spool {
        title: String,
        document: std::os::fd::OwnedFd,
        token: u32,
        reply: oneshot::Sender<bool>,
    },
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
        }
    }
}

/// The print host component.
pub(crate) struct Print {
    prepared: Rc<RefCell<HashMap<u32, Prepared>>>,
    next_token: Rc<RefCell<u32>>,
}

#[relm4::component(pub(crate))]
impl Component for Print {
    type Init = ();
    type Input = PrintInput;
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
        let model = Print {
            prepared: Rc::new(RefCell::new(HashMap::new())),
            next_token: Rc::new(RefCell::new(1)),
        };
        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: PrintInput, _sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            PrintInput::Prepare { title, reply } => self.prepare(&title, reply),
            PrintInput::Spool {
                title,
                document,
                token,
                reply,
            } => self.spool(&title, document, token, reply),
        }
    }
}

impl Print {
    /// Shows the print dialog and stores the selection under a fresh token.
    // PrintUnixDialog subclasses GtkDialog (response API deprecated since 4.10),
    // but it is still the only native print dialog GTK4 ships.
    #[allow(deprecated)]
    fn prepare(&self, title: &str, reply: PrepareReply) {
        let dialog = gtk::PrintUnixDialog::new(Some(title), gtk::Window::NONE);
        dialog.set_modal(true);

        let prepared = self.prepared.clone();
        let next_token = self.next_token.clone();
        let reply = Rc::new(RefCell::new(Some(reply)));

        dialog.connect_response(move |dialog, response| {
            let take = || reply.borrow_mut().take();
            match response {
                gtk::ResponseType::Ok | gtk::ResponseType::Apply => {
                    if let Some(printer) = dialog.selected_printer() {
                        let settings = dialog.settings();
                        let page_setup = dialog.page_setup();
                        let token = {
                            let mut counter = next_token.borrow_mut();
                            let token = *counter;
                            *counter = counter.wrapping_add(1).max(1);
                            token
                        };
                        let pairs = settings_pairs(&settings);
                        prepared.borrow_mut().insert(
                            token,
                            Prepared {
                                printer,
                                settings,
                                page_setup,
                            },
                        );
                        if let Some(reply) = take() {
                            let _ = reply.send(Some((pairs, token)));
                        }
                    } else if let Some(reply) = take() {
                        let _ = reply.send(None);
                    }
                }
                _ => {
                    if let Some(reply) = take() {
                        let _ = reply.send(None);
                    }
                }
            }
            dialog.destroy();
        });

        dialog.present();
    }

    /// Spools the document fd to the printer prepared under `token`.
    fn spool(
        &self,
        title: &str,
        document: std::os::fd::OwnedFd,
        token: u32,
        reply: oneshot::Sender<bool>,
    ) {
        let Some(prepared) = self.prepared.borrow_mut().remove(&token) else {
            warn!(token, "print: no prepared job for token");
            let _ = reply.send(false);
            return;
        };

        let job = gtk::PrintJob::new(
            title,
            &prepared.printer,
            &prepared.settings,
            &prepared.page_setup,
        );
        if let Err(err) = job.set_source_fd(document.as_raw_fd()) {
            warn!(%err, "print: cannot set document source");
            let _ = reply.send(false);
            return;
        }

        // Keep the fd alive until the job finishes sending.
        let reply = RefCell::new(Some(reply));
        job.send(move |_job, result| {
            let _ = &document;
            if let Some(reply) = reply.borrow_mut().take() {
                let _ = reply.send(result.is_ok());
            }
        });
    }
}

/// Flattens `GtkPrintSettings` into key/value string pairs for the portal.
fn settings_pairs(settings: &gtk::PrintSettings) -> Vec<(String, String)> {
    let pairs = Rc::new(RefCell::new(Vec::new()));
    let collector = pairs.clone();
    settings.foreach(move |key, value| {
        collector
            .borrow_mut()
            .push((key.to_owned(), value.to_owned()));
    });
    Rc::try_unwrap(pairs)
        .map(RefCell::into_inner)
        .unwrap_or_default()
}
