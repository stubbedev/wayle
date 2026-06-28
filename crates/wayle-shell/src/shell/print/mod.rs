//! Print — a custom animated layer-shell surface.
//!
//! Replaces `GtkPrintUnixDialog` with our own printer picker + settings form so
//! the portal print prompt animates congruently (`AnimSurface::Print`).
//! `Prepare` shows the form and stashes the chosen printer + settings under a
//! token; `Spool` re-resolves that printer and sends the document fd via
//! `GtkPrintJob`, honouring the stashed settings. Backs `com.wayle.Print1`.

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

/// Flat GTK print-setting key/value pairs returned to the portal frontend.
pub(crate) type SettingsPairs = Vec<(String, String)>;
/// Reply for a prepare request: `Some((settings, token))` or `None` on cancel.
type PrepareReply = oneshot::Sender<Option<(SettingsPairs, u32)>>;

/// Common paper sizes offered in the form, as `(label, PWG name)` pairs.
const PAPER_SIZES: &[(&str, &str)] = &[
    ("A4", "iso_a4"),
    ("Letter", "na_letter"),
    ("Legal", "na_legal"),
    ("A3", "iso_a3"),
    ("A5", "iso_a5"),
    ("Executive", "na_executive"),
];

/// A printer's display metadata for one list row.
struct PrinterMeta {
    name: String,
    location: String,
    status: String,
}

/// A prepared job stashed under a token until the frontend calls `print`.
///
/// Holds only `Send` data (no GTK objects) so it can be moved into the
/// `enumerate_printers` callback, which requires `Send + Sync`. The
/// `PrintSettings`/`PageSetup` are rebuilt from this inside the callback.
struct PreparedJob {
    printer_name: String,
    settings_pairs: SettingsPairs,
    paper_name: String,
    landscape: bool,
}

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
    /// Internal: the user confirmed the selected printer + settings.
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
    tokens: HashMap<u32, PreparedJob>,
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
                        set_vexpand: false,
                        set_min_content_height: 64,
                        set_max_content_height: 240,
                        set_propagate_natural_height: true,
                        #[name = "printer_list"]
                        gtk::ListBox {
                            add_css_class: "print-printer-list",
                            set_selection_mode: gtk::SelectionMode::Single,
                        },
                    },

                    // --- Settings form ------------------------------------
                    gtk::Grid {
                        add_css_class: "print-form",
                        set_row_spacing: 8,
                        set_column_spacing: 12,

                        attach[0, 0, 1, 1] = &gtk::Label {
                            add_css_class: "print-form-label",
                            set_xalign: 0.0,
                            set_label: "Copies",
                        },
                        #[name = "copies_spin"]
                        attach[1, 0, 1, 1] = &gtk::SpinButton {
                            set_hexpand: true,
                            set_adjustment: &gtk::Adjustment::new(1.0, 1.0, 999.0, 1.0, 10.0, 0.0),
                            set_digits: 0,
                        },

                        attach[0, 1, 1, 1] = &gtk::Label {
                            add_css_class: "print-form-label",
                            set_xalign: 0.0,
                            set_label: "Pages",
                        },
                        #[name = "range_entry"]
                        attach[1, 1, 1, 1] = &gtk::Entry {
                            set_hexpand: true,
                            set_placeholder_text: Some("All pages — or e.g. 1-5, 8"),
                        },

                        attach[0, 2, 1, 1] = &gtk::Label {
                            add_css_class: "print-form-label",
                            set_xalign: 0.0,
                            set_label: "Orientation",
                        },
                        #[name = "orientation_drop"]
                        attach[1, 2, 1, 1] = &gtk::DropDown {
                            set_hexpand: true,
                            set_model: Some(&gtk::StringList::new(&["Portrait", "Landscape"])),
                        },

                        attach[0, 3, 1, 1] = &gtk::Label {
                            add_css_class: "print-form-label",
                            set_xalign: 0.0,
                            set_label: "Paper size",
                        },
                        #[name = "paper_drop"]
                        attach[1, 3, 1, 1] = &gtk::DropDown {
                            set_hexpand: true,
                            set_model: Some(&paper_size_model()),
                        },

                        attach[0, 4, 1, 1] = &gtk::Label {
                            add_css_class: "print-form-label",
                            set_xalign: 0.0,
                            set_label: "Color",
                        },
                        #[name = "color_drop"]
                        attach[1, 4, 1, 1] = &gtk::DropDown {
                            set_hexpand: true,
                            set_model: Some(&gtk::StringList::new(&["Color", "Grayscale"])),
                        },

                        attach[0, 5, 1, 1] = &gtk::Label {
                            add_css_class: "print-form-label",
                            set_xalign: 0.0,
                            set_label: "Two-sided",
                        },
                        #[name = "duplex_drop"]
                        attach[1, 5, 1, 1] = &gtk::DropDown {
                            set_hexpand: true,
                            set_model: Some(&gtk::StringList::new(&[
                                "One-sided",
                                "Two-sided (long edge)",
                                "Two-sided (short edge)",
                            ])),
                        },

                        attach[0, 6, 1, 1] = &gtk::Label {
                            add_css_class: "print-form-label",
                            set_xalign: 0.0,
                            set_label: "Quality",
                        },
                        #[name = "quality_drop"]
                        attach[1, 6, 1, 1] = &gtk::DropDown {
                            set_hexpand: true,
                            set_model: Some(&gtk::StringList::new(&["Normal", "Draft", "High"])),
                        },
                    },

                    gtk::Box {
                        set_orientation: gtk::Orientation::Horizontal,
                        set_halign: gtk::Align::End,
                        set_spacing: 8,
                        #[name = "cancel_button"]
                        gtk::Button {
                            set_label: "Cancel",
                            add_css_class: "portal-dialog-cancel",
                            connect_clicked => PrintInput::Cancel,
                        },
                        #[name = "confirm_button"]
                        gtk::Button {
                            set_label: "Print",
                            add_css_class: "portal-dialog-confirm",
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
                let metas = enumerate_printers();
                self.printers = metas.iter().map(|m| m.name.clone()).collect();

                clear_list(&widgets.printer_list);
                for meta in &metas {
                    widgets.printer_list.append(&printer_row(meta));
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
                        let (pairs, paper_name, landscape) = build_settings(widgets);
                        let token = self.next_token;
                        self.next_token = self.next_token.wrapping_add(1).max(1);
                        self.tokens.insert(
                            token,
                            PreparedJob {
                                printer_name: printer,
                                settings_pairs: pairs.clone(),
                                paper_name,
                                landscape,
                            },
                        );
                        let _ = reply.send(Some((pairs, token)));
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
                let job = self.tokens.remove(&token);
                let _ = reply.send(match job {
                    Some(job) => spool(&title, document, &job),
                    None => {
                        warn!(token, "print: no prepared printer for token");
                        false
                    }
                });
            }
        }
    }
}

/// Reads the form controls into `(settings_pairs, paper_name, landscape)` — all
/// `Send` so the result can cross into the `enumerate_printers` callback. The
/// pairs are the flattened `PrintSettings` (also returned to the portal
/// frontend); paper + orientation are kept out for rebuilding the `PageSetup`.
fn build_settings(widgets: &PrintWidgets) -> (SettingsPairs, String, bool) {
    let settings = gtk::PrintSettings::new();

    settings.set_n_copies(widgets.copies_spin.value_as_int().max(1));

    let landscape = widgets.orientation_drop.selected() == 1;
    settings.set_orientation(if landscape {
        gtk::PageOrientation::Landscape
    } else {
        gtk::PageOrientation::Portrait
    });

    let paper_idx = widgets.paper_drop.selected() as usize;
    let paper_name = PAPER_SIZES.get(paper_idx).map_or("iso_a4", |p| p.1).to_owned();
    settings.set_paper_size(&gtk::PaperSize::new(Some(&paper_name)));

    settings.set_use_color(widgets.color_drop.selected() == 0);

    settings.set_duplex(match widgets.duplex_drop.selected() {
        1 => gtk::PrintDuplex::Horizontal,
        2 => gtk::PrintDuplex::Vertical,
        _ => gtk::PrintDuplex::Simplex,
    });

    settings.set_quality(match widgets.quality_drop.selected() {
        1 => gtk::PrintQuality::Draft,
        2 => gtk::PrintQuality::High,
        _ => gtk::PrintQuality::Normal,
    });

    let range = widgets.range_entry.text();
    if let Some(ranges) = parse_page_ranges(range.trim()) {
        settings.set("print-pages", Some("ranges"));
        settings.set("page-ranges", Some(ranges.as_str()));
    } else {
        settings.set("print-pages", Some("all"));
    }

    (settings_pairs(&settings), paper_name, landscape)
}

/// Parses a user page range like `1-5, 8` into GTK's zero-based `"0-4,7"`
/// string. Returns `None` for an empty input (meaning "all pages").
fn parse_page_ranges(input: &str) -> Option<String> {
    if input.is_empty() {
        return None;
    }
    let mut parts = Vec::new();
    for token in input.split(',') {
        let token = token.trim();
        if token.is_empty() {
            continue;
        }
        if let Some((start, end)) = token.split_once('-') {
            let start = start.trim().parse::<u32>().ok()?.max(1) - 1;
            let end = end.trim().parse::<u32>().ok()?.max(1) - 1;
            parts.push(format!("{start}-{end}"));
        } else {
            let page = token.parse::<u32>().ok()?.max(1) - 1;
            parts.push(format!("{page}-{page}"));
        }
    }
    (!parts.is_empty()).then(|| parts.join(","))
}

/// Flattens a `PrintSettings` into the `Vec<(key, value)>` the portal returns.
fn settings_pairs(settings: &gtk::PrintSettings) -> SettingsPairs {
    let pairs = Arc::new(Mutex::new(Vec::new()));
    let collector = Arc::clone(&pairs);
    settings.foreach(move |key, value| {
        if let Ok(mut pairs) = collector.lock() {
            pairs.push((key.to_owned(), value.to_owned()));
        }
    });
    Arc::try_unwrap(pairs)
        .ok()
        .and_then(|m| m.into_inner().ok())
        .unwrap_or_default()
}

/// The paper-size dropdown's string model.
fn paper_size_model() -> gtk::StringList {
    let labels: Vec<&str> = PAPER_SIZES.iter().map(|p| p.0).collect();
    gtk::StringList::new(&labels)
}

/// Builds a printer list row: bold name over a muted "location · status" line.
fn printer_row(meta: &PrinterMeta) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .margin_top(6)
        .margin_bottom(6)
        .margin_start(10)
        .margin_end(10)
        .build();
    row.append(
        &gtk::Label::builder()
            .label(&meta.name)
            .xalign(0.0)
            .css_classes(["print-printer-name"])
            .build(),
    );
    let detail = [meta.location.as_str(), meta.status.as_str()]
        .into_iter()
        .filter(|s| !s.is_empty())
        .collect::<Vec<_>>()
        .join(" · ");
    if !detail.is_empty() {
        row.append(
            &gtk::Label::builder()
                .label(&detail)
                .xalign(0.0)
                .css_classes(["print-printer-detail"])
                .build(),
        );
    }
    row
}

/// Enumerates printers with display metadata (synchronous).
fn enumerate_printers() -> Vec<PrinterMeta> {
    let metas = Arc::new(Mutex::new(Vec::new()));
    let collector = Arc::clone(&metas);
    gtk::enumerate_printers(
        move |printer| {
            // `location()` / `state_message()` wrap the C getter with
            // `from_glib_full`, which panics on NULL — and virtual printers
            // (e.g. "Print to File") return NULL here. Read them as nullable
            // properties instead so a missing value is just an empty string.
            let location = nullable_string(printer, "location");
            let state_msg = nullable_string(printer, "state-message");
            let status = if printer.is_paused() {
                "Paused".to_owned()
            } else if !printer.is_accepting_jobs() {
                "Not accepting jobs".to_owned()
            } else if !state_msg.is_empty() {
                state_msg
            } else {
                "Ready".to_owned()
            };
            if let Ok(mut metas) = collector.lock() {
                metas.push(PrinterMeta {
                    name: printer.name().to_string(),
                    location,
                    status,
                });
            }
            true
        },
        true,
    );
    Arc::try_unwrap(metas)
        .ok()
        .and_then(|m| m.into_inner().ok())
        .unwrap_or_default()
}

/// Reads a nullable string property off a printer without the NULL-panicking
/// `from_glib_full` path the typed getters use. Returns `""` when absent.
fn nullable_string(printer: &gtk::Printer, prop: &str) -> String {
    printer
        .property_value(prop)
        .get::<Option<String>>()
        .ok()
        .flatten()
        .unwrap_or_default()
}

/// Spools `document` to the prepared printer with the chosen settings via a
/// `GtkPrintJob`. The `PrintSettings`/`PageSetup` are rebuilt inside the
/// callback (it must be `Send + Sync`, and GTK objects are neither). Returns
/// whether a matching printer was found and queued.
fn spool(title: &str, document: OwnedFd, job: &PreparedJob) -> bool {
    // The print job reads the fd asynchronously while spooling, so hand it the
    // raw fd and let the job own it (leaked from our side intentionally).
    let raw = document.into_raw_fd();
    let title = title.to_owned();
    let target = job.printer_name.clone();
    let pairs = job.settings_pairs.clone();
    let paper_name = job.paper_name.clone();
    let landscape = job.landscape;
    let sent = Arc::new(AtomicBool::new(false));
    let sent_cb = Arc::clone(&sent);

    gtk::enumerate_printers(
        move |printer| {
            if printer.name() != target.as_str() {
                return true;
            }
            let settings = gtk::PrintSettings::new();
            for (key, value) in &pairs {
                settings.set(key, Some(value.as_str()));
            }
            let page_setup = gtk::PageSetup::new();
            page_setup.set_orientation(if landscape {
                gtk::PageOrientation::Landscape
            } else {
                gtk::PageOrientation::Portrait
            });
            page_setup.set_paper_size(&gtk::PaperSize::new(Some(&paper_name)));

            let job = gtk::PrintJob::new(&title, printer, &settings, &page_setup);
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

#[cfg(test)]
mod tests {
    use super::parse_page_ranges;

    #[test]
    fn empty_means_all() {
        assert_eq!(parse_page_ranges(""), None);
        assert_eq!(parse_page_ranges("   "), None);
    }

    #[test]
    fn converts_to_zero_based() {
        assert_eq!(parse_page_ranges("1-5, 8").as_deref(), Some("0-4,7-7"));
        assert_eq!(parse_page_ranges("3").as_deref(), Some("2-2"));
    }

    #[test]
    fn rejects_garbage() {
        assert_eq!(parse_page_ranges("abc"), None);
        assert_eq!(parse_page_ranges("1-x"), None);
    }
}
