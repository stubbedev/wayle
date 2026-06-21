//! File chooser — a custom animated layer-shell file browser.
//!
//! Replaces the native `gtk::FileDialog` with our own surface so the portal
//! file picker animates congruently (`AnimSurface::FileChooser`) like the rest
//! of the shell. Backs `com.wayle.FileChooser1`: open file(s) / pick a folder /
//! save, with the portal's filters + starting folder. Returns `file://` URIs.

use std::{
    path::{Path, PathBuf},
    sync::Arc,
};

use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{
    gtk,
    gtk::{gio, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;
use wayle_config::{ConfigService, schemas::animations::AnimSurface};

use crate::shell::helpers::surface_anim;

/// A file filter: a display name and `(kind, value)` rules where kind 0 = glob
/// pattern, 1 = MIME type.
pub(crate) type Filter = (String, Vec<(u32, String)>);

/// Messages driving the file chooser host.
pub(crate) enum FileChooserInput {
    /// Open existing file(s) or a directory.
    Open {
        title: String,
        multiple: bool,
        directory: bool,
        filters: Vec<Filter>,
        current_folder: String,
        reply: oneshot::Sender<Vec<String>>,
    },
    /// Choose a save destination seeded with `current_name`.
    Save {
        title: String,
        current_name: String,
        filters: Vec<Filter>,
        current_folder: String,
        reply: oneshot::Sender<Vec<String>>,
    },
    /// Internal: a list row was activated.
    Activate(u32),
    /// Internal: go to the parent directory.
    GoUp,
    /// Internal: confirm the current selection.
    Confirm,
    /// Internal: cancel.
    Cancel,
}

impl std::fmt::Debug for FileChooserInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Open { title, .. } => f
                .debug_struct("Open")
                .field("title", title)
                .finish_non_exhaustive(),
            Self::Save { title, .. } => f
                .debug_struct("Save")
                .field("title", title)
                .finish_non_exhaustive(),
            Self::Activate(i) => f.debug_tuple("Activate").field(i).finish(),
            Self::GoUp => f.write_str("GoUp"),
            Self::Confirm => f.write_str("Confirm"),
            Self::Cancel => f.write_str("Cancel"),
        }
    }
}

/// What the chooser is doing.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Mode {
    OpenFile,
    OpenMultiple,
    Folder,
    Save,
}

/// An entry in the current directory listing.
struct Entry {
    path: PathBuf,
    is_dir: bool,
}

/// Active request state.
struct Active {
    mode: Mode,
    dir: PathBuf,
    filters: Vec<Filter>,
    entries: Vec<Entry>,
    reply: oneshot::Sender<Vec<String>>,
}

/// The file chooser host component.
pub(crate) struct FileChooser {
    config: Arc<ConfigService>,
    active: Option<Active>,
}

#[relm4::component(pub(crate))]
impl Component for FileChooser {
    type Init = Arc<ConfigService>;
    type Input = FileChooserInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            add_css_class: "file-chooser-window",
            set_visible: false,

            #[name = "revealer"]
            gtk::Revealer {
                set_reveal_child: false,

                gtk::Box {
                    add_css_class: "file-chooser-surface",
                    set_orientation: gtk::Orientation::Vertical,
                    set_spacing: 8,
                    set_width_request: 680,
                    set_height_request: 540,

                    #[name = "title_label"]
                    gtk::Label {
                        add_css_class: "file-chooser-title",
                        set_xalign: 0.0,
                    },
                    gtk::Box {
                        set_orientation: gtk::Orientation::Horizontal,
                        set_spacing: 8,
                        #[name = "up_button"]
                        gtk::Button {
                            set_label: "Up",
                            connect_clicked => FileChooserInput::GoUp,
                        },
                        #[name = "path_label"]
                        gtk::Label {
                            add_css_class: "file-chooser-path",
                            set_xalign: 0.0,
                            set_hexpand: true,
                            set_ellipsize: gtk::pango::EllipsizeMode::Start,
                        },
                    },
                    gtk::ScrolledWindow {
                        set_vexpand: true,
                        #[name = "file_list"]
                        gtk::ListBox {
                            add_css_class: "file-chooser-list",
                            set_selection_mode: gtk::SelectionMode::Single,
                        },
                    },
                    #[name = "name_entry"]
                    gtk::Entry {
                        add_css_class: "file-chooser-name",
                        set_placeholder_text: Some("File name"),
                        set_visible: false,
                    },
                    gtk::Box {
                        set_orientation: gtk::Orientation::Horizontal,
                        set_halign: gtk::Align::End,
                        set_spacing: 8,
                        #[name = "cancel_button"]
                        gtk::Button {
                            set_label: "Cancel",
                            connect_clicked => FileChooserInput::Cancel,
                        },
                        #[name = "confirm_button"]
                        gtk::Button {
                            add_css_class: "suggested-action",
                            connect_clicked => FileChooserInput::Confirm,
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
        let model = FileChooser {
            config: init,
            active: None,
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-file-chooser"));
        root.set_layer(Layer::Overlay);
        root.set_keyboard_mode(KeyboardMode::OnDemand);
        root.set_exclusive_zone(-1);

        widgets.file_list.connect_row_activated({
            let input = sender.input_sender().clone();
            move |_, row| input.emit(FileChooserInput::Activate(row.index().max(0) as u32))
        });
        widgets.name_entry.connect_activate({
            let input = sender.input_sender().clone();
            move |_| input.emit(FileChooserInput::Confirm)
        });
        surface_anim::play_on_map(&root, &widgets.revealer);

        ComponentParts { model, widgets }
    }

    fn update_with_view(
        &mut self,
        widgets: &mut Self::Widgets,
        msg: FileChooserInput,
        _sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            FileChooserInput::Open {
                title,
                multiple,
                directory,
                filters,
                current_folder,
                reply,
            } => {
                let mode = if directory {
                    Mode::Folder
                } else if multiple {
                    Mode::OpenMultiple
                } else {
                    Mode::OpenFile
                };
                self.begin(
                    widgets,
                    root,
                    &title,
                    mode,
                    filters,
                    &current_folder,
                    "",
                    reply,
                );
            }
            FileChooserInput::Save {
                title,
                current_name,
                filters,
                current_folder,
                reply,
            } => {
                self.begin(
                    widgets,
                    root,
                    &title,
                    Mode::Save,
                    filters,
                    &current_folder,
                    &current_name,
                    reply,
                );
            }
            FileChooserInput::Activate(index) => self.activate(widgets, index),
            FileChooserInput::GoUp => self.go_up(widgets),
            FileChooserInput::Confirm => self.confirm(widgets, root),
            FileChooserInput::Cancel => {
                if let Some(active) = self.active.take() {
                    let _ = active.reply.send(Vec::new());
                }
                surface_anim::hide(
                    &widgets.revealer,
                    root,
                    &self.config,
                    AnimSurface::FileChooser,
                );
            }
        }
    }
}

impl FileChooser {
    #[allow(clippy::too_many_arguments)]
    fn begin(
        &mut self,
        widgets: &FileChooserWidgets,
        root: &gtk::Window,
        title: &str,
        mode: Mode,
        filters: Vec<Filter>,
        current_folder: &str,
        current_name: &str,
        reply: oneshot::Sender<Vec<String>>,
    ) {
        if let Some(active) = self.active.take() {
            let _ = active.reply.send(Vec::new());
        }
        let dir = start_dir(current_folder);
        self.active = Some(Active {
            mode,
            dir,
            filters,
            entries: Vec::new(),
            reply,
        });

        widgets.title_label.set_label(title);
        widgets
            .file_list
            .set_selection_mode(if mode == Mode::OpenMultiple {
                gtk::SelectionMode::Multiple
            } else {
                gtk::SelectionMode::Single
            });
        widgets.name_entry.set_visible(mode == Mode::Save);
        widgets.name_entry.set_text(current_name);
        widgets.confirm_button.set_label(match mode {
            Mode::Save => "Save",
            Mode::Folder => "Select",
            _ => "Open",
        });

        self.populate(widgets);
        surface_anim::reveal(
            &widgets.revealer,
            root,
            &self.config,
            AnimSurface::FileChooser,
        );
    }

    /// Reads the active directory and repaints the list.
    fn populate(&mut self, widgets: &FileChooserWidgets) {
        let Some(active) = self.active.as_mut() else {
            return;
        };
        clear_list(&widgets.file_list);
        active.entries = list_dir(&active.dir, &active.filters, active.mode);

        widgets.path_label.set_label(&active.dir.to_string_lossy());
        for entry in &active.entries {
            let name = entry
                .path
                .file_name()
                .map(|n| n.to_string_lossy().into_owned())
                .unwrap_or_default();
            let label = gtk::Label::builder()
                .label(if entry.is_dir {
                    format!("{name}/")
                } else {
                    name
                })
                .xalign(0.0)
                .margin_top(4)
                .margin_bottom(4)
                .margin_start(8)
                .margin_end(8)
                .build();
            widgets.file_list.append(&label);
        }
    }

    /// Handles a row activation: descend into dirs, or pick/seed for files.
    fn activate(&mut self, widgets: &FileChooserWidgets, index: u32) {
        let Some(active) = self.active.as_mut() else {
            return;
        };
        let Some(entry) = active.entries.get(index as usize) else {
            return;
        };
        if entry.is_dir {
            active.dir = entry.path.clone();
            self.populate(widgets);
        } else if active.mode == Mode::Save
            && let Some(name) = entry.path.file_name()
        {
            widgets.name_entry.set_text(&name.to_string_lossy());
        }
        // For open modes a single click already selects the row; Confirm reads it.
    }

    fn go_up(&mut self, widgets: &FileChooserWidgets) {
        if let Some(active) = self.active.as_mut()
            && let Some(parent) = active.dir.parent()
        {
            active.dir = parent.to_path_buf();
            self.populate(widgets);
        }
    }

    fn confirm(&mut self, widgets: &FileChooserWidgets, root: &gtk::Window) {
        let Some(active) = self.active.take() else {
            return;
        };
        let uris = match active.mode {
            Mode::Folder => vec![uri_of(&active.dir)],
            Mode::Save => {
                let name = widgets.name_entry.text();
                if name.is_empty() {
                    self.active = Some(active);
                    return;
                }
                vec![uri_of(&active.dir.join(name.as_str()))]
            }
            Mode::OpenFile | Mode::OpenMultiple => {
                let selected = selected_files(&widgets.file_list, &active.entries);
                if selected.is_empty() {
                    self.active = Some(active);
                    return;
                }
                selected
            }
        };
        let _ = active.reply.send(uris);
        surface_anim::hide(
            &widgets.revealer,
            root,
            &self.config,
            AnimSurface::FileChooser,
        );
    }
}

/// The `file://` URIs of the selected (file) rows.
fn selected_files(list: &gtk::ListBox, entries: &[Entry]) -> Vec<String> {
    list.selected_rows()
        .iter()
        .filter_map(|row| entries.get(usize::try_from(row.index()).ok()?))
        .filter(|entry| !entry.is_dir)
        .map(|entry| uri_of(&entry.path))
        .collect()
}

/// Resolves the starting directory: the requested folder, else `$HOME`, else `/`.
fn start_dir(current_folder: &str) -> PathBuf {
    if !current_folder.is_empty() {
        let path = PathBuf::from(current_folder);
        if path.is_dir() {
            return path;
        }
    }
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("/"))
}

/// Lists `dir`: directories first, then files (filtered, unless picking a
/// folder). Hidden entries are skipped.
fn list_dir(dir: &Path, filters: &[Filter], mode: Mode) -> Vec<Entry> {
    let Ok(read) = std::fs::read_dir(dir) else {
        return Vec::new();
    };
    let mut dirs = Vec::new();
    let mut files = Vec::new();
    for entry in read.flatten() {
        let path = entry.path();
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if name.starts_with('.') {
            continue;
        }
        let is_dir = path.is_dir();
        if is_dir {
            dirs.push(Entry { path, is_dir });
        } else if mode != Mode::Folder && matches_filters(&name, filters) {
            files.push(Entry { path, is_dir });
        }
    }
    dirs.sort_by(|a, b| a.path.cmp(&b.path));
    files.sort_by(|a, b| a.path.cmp(&b.path));
    dirs.append(&mut files);
    dirs
}

/// Whether `name` matches any filter rule (empty filters = match all). Globs are
/// matched by `*.ext` suffix or `*`; MIME-type rules are ignored here.
fn matches_filters(name: &str, filters: &[Filter]) -> bool {
    if filters.is_empty() {
        return true;
    }
    filters
        .iter()
        .flat_map(|(_, rules)| rules)
        .any(|(kind, value)| {
            if *kind != 0 {
                return false; // MIME rules are not matched here.
            }
            if value == "*" {
                true
            } else if let Some(suffix) = value.strip_prefix('*') {
                name.ends_with(suffix)
            } else {
                name == value
            }
        })
}

/// `file://` URI for a path.
fn uri_of(path: &Path) -> String {
    gio::File::for_path(path).uri().to_string()
}

/// Removes all rows from a list box.
fn clear_list(list: &gtk::ListBox) {
    while let Some(child) = list.first_child() {
        list.remove(&child);
    }
}
