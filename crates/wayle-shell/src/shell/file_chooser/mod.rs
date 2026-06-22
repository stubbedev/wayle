//! File chooser — a custom animated layer-shell file browser.
//!
//! Replaces the native `gtk::FileDialog` with our own surface so the portal
//! file picker animates congruently (`AnimSurface::FileChooser`) like the rest
//! of the shell. Backs `com.wayle.FileChooser1`: open file(s) / pick a folder /
//! save, with the portal's filters + starting folder. Returns `file://` URIs.

use std::{
    cmp::Ordering,
    path::{Component as PathComponent, Path, PathBuf},
    sync::Arc,
    time::SystemTime,
};

use gtk4_layer_shell::{KeyboardMode, Layer, LayerShell};
use relm4::{
    Sender, gtk,
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
    /// Internal: a favorite sidebar place was activated.
    Place(u32),
    /// Internal: an "other location" sidebar entry was activated.
    Location(u32),
    /// Internal: the search query changed.
    Search(String),
    /// Internal: a breadcrumb segment was activated — jump to that ancestor.
    Crumb(PathBuf),
    /// Internal: go to the parent directory.
    GoUp,
    /// Internal: the show-hidden-files toggle changed.
    ToggleHidden,
    /// Internal: the active file-type filter changed.
    SelectFilter(u32),
    /// Internal: a column header was clicked — sort by it (toggling direction).
    Sort(SortColumn),
    /// Internal: toggle between list and grid view.
    ToggleView,
    /// Internal: toggle the Quick Look preview of the selected file (spacebar).
    ToggleQuickLook,
    /// Internal: toggle the persistent side preview pane.
    TogglePreview,
    /// Internal: the file selection changed — refresh the preview pane.
    SelectionChanged,
    /// Internal: a path was dropped on the surface — navigate to it.
    Dropped(PathBuf),
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
            Self::Place(i) => f.debug_tuple("Place").field(i).finish(),
            Self::Location(i) => f.debug_tuple("Location").field(i).finish(),
            Self::Search(q) => f.debug_tuple("Search").field(q).finish(),
            Self::Crumb(p) => f.debug_tuple("Crumb").field(p).finish(),
            Self::GoUp => f.write_str("GoUp"),
            Self::ToggleHidden => f.write_str("ToggleHidden"),
            Self::SelectFilter(i) => f.debug_tuple("SelectFilter").field(i).finish(),
            Self::Sort(_) => f.write_str("Sort"),
            Self::ToggleView => f.write_str("ToggleView"),
            Self::ToggleQuickLook => f.write_str("ToggleQuickLook"),
            Self::TogglePreview => f.write_str("TogglePreview"),
            Self::SelectionChanged => f.write_str("SelectionChanged"),
            Self::Dropped(p) => f.debug_tuple("Dropped").field(p).finish(),
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
    size: u64,
    modified: Option<SystemTime>,
}

/// A sortable column in the list view.
#[derive(Clone, Copy, PartialEq, Eq)]
pub(crate) enum SortColumn {
    Name,
    Size,
    Modified,
}

/// How entries are currently displayed.
#[derive(Clone, Copy, PartialEq, Eq)]
enum ViewMode {
    List,
    Grid,
}

/// A sidebar shortcut: a label, the directory it jumps to, and its icon name.
struct Place {
    label: String,
    path: PathBuf,
    icon: &'static str,
}

/// Active request state.
struct Active {
    mode: Mode,
    dir: PathBuf,
    filters: Vec<Filter>,
    /// Index into `filters` of the active file-type filter (ignored if empty).
    active_filter: usize,
    entries: Vec<Entry>,
    reply: oneshot::Sender<Vec<String>>,
}

/// The file chooser host component.
pub(crate) struct FileChooser {
    config: Arc<ConfigService>,
    active: Option<Active>,
    places: Vec<Place>,
    locations: Vec<Place>,
    show_hidden: bool,
    search: String,
    sort_key: SortColumn,
    sort_asc: bool,
    view: ViewMode,
    input: Sender<FileChooserInput>,
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
                    set_spacing: 0,
                    set_width_request: 760,
                    set_height_request: 520,

                    // --- Header: nav + centered title + hidden toggle ---
                    gtk::CenterBox {
                        add_css_class: "file-chooser-header",
                        #[wrap(Some)]
                        set_start_widget = &gtk::Box {
                            set_orientation: gtk::Orientation::Horizontal,
                            set_spacing: 2,
                            #[name = "up_button"]
                            gtk::Button {
                                add_css_class: "file-chooser-nav",
                                add_css_class: "flat",
                                set_icon_name: "go-up-symbolic",
                                set_tooltip_text: Some("Up"),
                                connect_clicked => FileChooserInput::GoUp,
                            },
                        },
                        #[wrap(Some)]
                        #[name = "title_label"]
                        set_center_widget = &gtk::Label {
                            add_css_class: "file-chooser-title",
                        },
                        #[wrap(Some)]
                        set_end_widget = &gtk::Box {
                            set_orientation: gtk::Orientation::Horizontal,
                            set_spacing: 2,
                            #[name = "view_toggle"]
                            gtk::Button {
                                add_css_class: "file-chooser-nav",
                                add_css_class: "flat",
                                set_icon_name: "view-grid-symbolic",
                                set_tooltip_text: Some("Toggle grid view"),
                                connect_clicked => FileChooserInput::ToggleView,
                            },
                            #[name = "preview_toggle"]
                            gtk::ToggleButton {
                                add_css_class: "file-chooser-nav",
                                add_css_class: "flat",
                                set_icon_name: "view-paged-symbolic",
                                set_tooltip_text: Some("Preview pane"),
                                connect_toggled => FileChooserInput::TogglePreview,
                            },
                            #[name = "hidden_toggle"]
                            gtk::ToggleButton {
                                add_css_class: "file-chooser-nav",
                                add_css_class: "flat",
                                set_icon_name: "view-reveal-symbolic",
                                set_tooltip_text: Some("Show hidden files"),
                                connect_toggled => FileChooserInput::ToggleHidden,
                            },
                        },
                    },

                    // --- Path bar: breadcrumb + filter dropdown ---------
                    gtk::Box {
                        add_css_class: "file-chooser-crumbbar",
                        set_orientation: gtk::Orientation::Horizontal,
                        set_spacing: 8,
                        gtk::ScrolledWindow {
                            set_hexpand: true,
                            set_vscrollbar_policy: gtk::PolicyType::Never,
                            set_propagate_natural_height: true,
                            #[name = "crumb_bar"]
                            gtk::Box {
                                add_css_class: "file-chooser-crumbs",
                                set_orientation: gtk::Orientation::Horizontal,
                                set_spacing: 2,
                            },
                        },
                        #[name = "search_entry"]
                        gtk::SearchEntry {
                            add_css_class: "file-chooser-search",
                            set_placeholder_text: Some("Search"),
                            set_width_request: 150,
                            set_valign: gtk::Align::Center,
                        },
                        #[name = "filter_dropdown"]
                        gtk::DropDown {
                            add_css_class: "file-chooser-filter",
                            set_visible: false,
                        },
                    },

                    // --- Body: sidebar + file list ----------------------
                    gtk::Box {
                        add_css_class: "file-chooser-body",
                        set_orientation: gtk::Orientation::Horizontal,
                        set_spacing: 0,
                        set_vexpand: true,

                        gtk::ScrolledWindow {
                            add_css_class: "file-chooser-sidebar-scroll",
                            set_hscrollbar_policy: gtk::PolicyType::Never,
                            set_width_request: 180,
                            gtk::Box {
                                add_css_class: "file-chooser-sidebar",
                                set_orientation: gtk::Orientation::Vertical,
                                set_spacing: 2,
                                gtk::Label {
                                    add_css_class: "file-chooser-sidebar-header",
                                    set_label: "Favorites",
                                    set_xalign: 0.0,
                                },
                                #[name = "places_list"]
                                gtk::ListBox {
                                    add_css_class: "file-chooser-places",
                                    set_selection_mode: gtk::SelectionMode::Single,
                                },
                                gtk::Label {
                                    add_css_class: "file-chooser-sidebar-header",
                                    set_label: "Locations",
                                    set_xalign: 0.0,
                                    set_margin_top: 8,
                                },
                                #[name = "locations_list"]
                                gtk::ListBox {
                                    add_css_class: "file-chooser-places",
                                    set_selection_mode: gtk::SelectionMode::Single,
                                },
                            },
                        },

                        gtk::Box {
                            set_orientation: gtk::Orientation::Vertical,
                            set_spacing: 0,
                            set_hexpand: true,
                            set_vexpand: true,

                            // Clickable column headers (list view only).
                            #[name = "col_header"]
                            gtk::Box {
                                add_css_class: "file-chooser-colheader",
                                set_orientation: gtk::Orientation::Horizontal,
                                set_spacing: 0,
                                #[name = "sort_name_btn"]
                                gtk::Button {
                                    add_css_class: "file-chooser-col",
                                    add_css_class: "flat",
                                    set_hexpand: true,
                                    set_label: "Name",
                                    connect_clicked => FileChooserInput::Sort(SortColumn::Name),
                                },
                                #[name = "sort_size_btn"]
                                gtk::Button {
                                    add_css_class: "file-chooser-col",
                                    add_css_class: "flat",
                                    set_width_request: 90,
                                    set_label: "Size",
                                    connect_clicked => FileChooserInput::Sort(SortColumn::Size),
                                },
                                #[name = "sort_modified_btn"]
                                gtk::Button {
                                    add_css_class: "file-chooser-col",
                                    add_css_class: "flat",
                                    set_width_request: 150,
                                    set_label: "Modified",
                                    connect_clicked => FileChooserInput::Sort(SortColumn::Modified),
                                },
                            },

                            #[name = "list_overlay"]
                            gtk::Overlay {
                                set_vexpand: true,
                                gtk::ScrolledWindow {
                                    add_css_class: "file-chooser-list-scroll",
                                    set_hexpand: true,
                                    set_vexpand: true,
                                    #[name = "file_list"]
                                    gtk::ListBox {
                                        add_css_class: "file-chooser-list",
                                        set_selection_mode: gtk::SelectionMode::Single,
                                    },
                                },
                                #[name = "empty_label"]
                                add_overlay = &gtk::Label {
                                    add_css_class: "file-chooser-empty",
                                    set_label: "No items",
                                    set_halign: gtk::Align::Center,
                                    set_valign: gtk::Align::Center,
                                    set_visible: false,
                                    set_can_target: false,
                                },
                                // Quick Look preview (spacebar) — a peek card over the list.
                                #[name = "quicklook"]
                                add_overlay = &gtk::Box {
                                    add_css_class: "file-chooser-quicklook",
                                    set_orientation: gtk::Orientation::Vertical,
                                    set_spacing: 12,
                                    set_halign: gtk::Align::Center,
                                    set_valign: gtk::Align::Center,
                                    set_visible: false,
                                    #[name = "ql_icon"]
                                    gtk::Box {
                                        add_css_class: "file-chooser-ql-icon",
                                        set_halign: gtk::Align::Center,
                                    },
                                    #[name = "ql_name"]
                                    gtk::Label {
                                        add_css_class: "file-chooser-ql-name",
                                        set_justify: gtk::Justification::Center,
                                        set_wrap: true,
                                        set_max_width_chars: 32,
                                    },
                                    #[name = "ql_info"]
                                    gtk::Label {
                                        add_css_class: "file-chooser-ql-info",
                                        set_justify: gtk::Justification::Center,
                                    },
                                },
                            },

                            #[name = "grid_scroll"]
                            gtk::ScrolledWindow {
                                add_css_class: "file-chooser-grid-scroll",
                                set_vexpand: true,
                                set_visible: false,
                                set_hscrollbar_policy: gtk::PolicyType::Never,
                                #[name = "file_grid"]
                                gtk::FlowBox {
                                    add_css_class: "file-chooser-grid",
                                    set_selection_mode: gtk::SelectionMode::Single,
                                    set_homogeneous: true,
                                    set_min_children_per_line: 3,
                                    set_max_children_per_line: 8,
                                    set_valign: gtk::Align::Start,
                                },
                            },
                        },

                        // Optional preview pane (right) — selected item preview.
                        #[name = "preview_pane"]
                        gtk::Box {
                            add_css_class: "file-chooser-preview",
                            set_orientation: gtk::Orientation::Vertical,
                            set_spacing: 12,
                            set_width_request: 220,
                            set_valign: gtk::Align::Center,
                            set_halign: gtk::Align::Center,
                            set_visible: false,
                            #[name = "preview_icon"]
                            gtk::Box {
                                add_css_class: "file-chooser-preview-icon",
                                set_halign: gtk::Align::Center,
                            },
                            #[name = "preview_name"]
                            gtk::Label {
                                add_css_class: "file-chooser-preview-name",
                                set_justify: gtk::Justification::Center,
                                set_wrap: true,
                                set_max_width_chars: 22,
                            },
                            #[name = "preview_info"]
                            gtk::Label {
                                add_css_class: "file-chooser-preview-info",
                                set_justify: gtk::Justification::Center,
                                set_wrap: true,
                            },
                        },
                    },

                    #[name = "name_entry"]
                    gtk::Entry {
                        add_css_class: "file-chooser-name",
                        set_placeholder_text: Some("File name"),
                        set_visible: false,
                    },

                    // --- Footer: actions --------------------------------
                    gtk::Box {
                        add_css_class: "file-chooser-footer",
                        set_orientation: gtk::Orientation::Horizontal,
                        set_spacing: 8,
                        gtk::Box { set_hexpand: true },
                        #[name = "cancel_button"]
                        gtk::Button {
                            add_css_class: "file-chooser-cancel",
                            set_label: "Cancel",
                            connect_clicked => FileChooserInput::Cancel,
                        },
                        #[name = "confirm_button"]
                        gtk::Button {
                            add_css_class: "file-chooser-confirm",
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
            places: user_places(),
            locations: other_locations(),
            show_hidden: false,
            search: String::new(),
            sort_key: SortColumn::Name,
            sort_asc: true,
            view: ViewMode::List,
            input: sender.input_sender().clone(),
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-file-chooser"));
        root.set_layer(Layer::Overlay);
        // Grab keyboard while shown so type-ahead, Esc, and arrow navigation work
        // immediately — like a native modal picker.
        root.set_keyboard_mode(KeyboardMode::Exclusive);
        root.set_exclusive_zone(-1);

        for place in &model.places {
            widgets
                .places_list
                .append(&place_row(&place.label, place.icon));
        }
        widgets.places_list.connect_row_activated({
            let input = sender.input_sender().clone();
            move |_, row| input.emit(FileChooserInput::Place(row.index().max(0) as u32))
        });

        for place in &model.locations {
            widgets
                .locations_list
                .append(&place_row(&place.label, place.icon));
        }
        widgets.locations_list.connect_row_activated({
            let input = sender.input_sender().clone();
            move |_, row| input.emit(FileChooserInput::Location(row.index().max(0) as u32))
        });

        widgets.search_entry.connect_search_changed({
            let input = sender.input_sender().clone();
            move |entry| input.emit(FileChooserInput::Search(entry.text().to_string()))
        });

        widgets.file_list.connect_row_activated({
            let input = sender.input_sender().clone();
            move |_, row| input.emit(FileChooserInput::Activate(row.index().max(0) as u32))
        });
        widgets.file_grid.connect_child_activated({
            let input = sender.input_sender().clone();
            move |_, child| input.emit(FileChooserInput::Activate(child.index().max(0) as u32))
        });
        widgets.file_list.connect_selected_rows_changed({
            let input = sender.input_sender().clone();
            move |_| input.emit(FileChooserInput::SelectionChanged)
        });
        widgets.file_grid.connect_selected_children_changed({
            let input = sender.input_sender().clone();
            move |_| input.emit(FileChooserInput::SelectionChanged)
        });
        widgets.name_entry.connect_activate({
            let input = sender.input_sender().clone();
            move |_| input.emit(FileChooserInput::Confirm)
        });
        widgets.filter_dropdown.connect_selected_notify({
            let input = sender.input_sender().clone();
            move |dd| {
                let i = dd.selected();
                if i != u32::MAX {
                    input.emit(FileChooserInput::SelectFilter(i));
                }
            }
        });

        // Hand cursor on every interactive element (GTK ignores CSS `cursor`).
        for widget in [
            widgets.up_button.upcast_ref::<gtk::Widget>(),
            widgets.hidden_toggle.upcast_ref(),
            widgets.filter_dropdown.upcast_ref(),
            widgets.cancel_button.upcast_ref(),
            widgets.confirm_button.upcast_ref(),
            widgets.view_toggle.upcast_ref(),
            widgets.preview_toggle.upcast_ref(),
            widgets.sort_name_btn.upcast_ref(),
            widgets.sort_size_btn.upcast_ref(),
            widgets.sort_modified_btn.upcast_ref(),
            widgets.places_list.upcast_ref(),
            widgets.locations_list.upcast_ref(),
            widgets.file_list.upcast_ref(),
            widgets.file_grid.upcast_ref(),
            widgets.crumb_bar.upcast_ref(),
        ] {
            widget.set_cursor_from_name(Some("pointer"));
        }

        install_controllers(
            &root,
            &widgets.revealer,
            &widgets.search_entry,
            sender.input_sender(),
        );

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
            FileChooserInput::Place(index) => {
                if let Some(place) = self.places.get(index as usize) {
                    self.goto_dir(widgets, place.path.clone());
                }
            }
            FileChooserInput::Location(index) => {
                if let Some(place) = self.locations.get(index as usize) {
                    self.goto_dir(widgets, place.path.clone());
                }
            }
            FileChooserInput::Search(query) => {
                self.search = query;
                self.populate(widgets);
            }
            FileChooserInput::Crumb(path) => self.goto_dir(widgets, path),
            FileChooserInput::GoUp => self.go_up(widgets),
            FileChooserInput::ToggleHidden => {
                self.show_hidden = widgets.hidden_toggle.is_active();
                self.populate(widgets);
            }
            FileChooserInput::SelectFilter(index) => {
                if let Some(active) = self.active.as_mut() {
                    active.active_filter = index as usize;
                }
                self.populate(widgets);
            }
            FileChooserInput::Sort(column) => self.sort_by(widgets, column),
            FileChooserInput::ToggleView => self.toggle_view(widgets),
            FileChooserInput::ToggleQuickLook => self.toggle_quicklook(widgets),
            FileChooserInput::TogglePreview => {
                widgets
                    .preview_pane
                    .set_visible(widgets.preview_toggle.is_active());
                self.refresh_preview(widgets);
            }
            FileChooserInput::SelectionChanged => self.refresh_preview(widgets),
            FileChooserInput::Dropped(path) => self.drop_path(widgets, path),
            FileChooserInput::Confirm => self.confirm(widgets, root),
            FileChooserInput::Cancel => self.cancel(widgets, root),
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

        // Populate the filter dropdown from the request's named filters.
        if filters.is_empty() {
            widgets.filter_dropdown.set_visible(false);
        } else {
            let names: Vec<&str> = filters.iter().map(|(name, _)| name.as_str()).collect();
            let model = gtk::StringList::new(&names);
            widgets.filter_dropdown.set_model(Some(&model));
            widgets.filter_dropdown.set_selected(0);
            widgets.filter_dropdown.set_visible(true);
        }

        self.active = Some(Active {
            mode,
            dir,
            filters,
            active_filter: 0,
            entries: Vec::new(),
            reply,
        });

        widgets.title_label.set_label(title);
        let selection = if mode == Mode::OpenMultiple {
            gtk::SelectionMode::Multiple
        } else {
            gtk::SelectionMode::Single
        };
        widgets.file_list.set_selection_mode(selection);
        widgets.file_grid.set_selection_mode(selection);
        widgets.name_entry.set_visible(mode == Mode::Save);
        widgets.name_entry.set_text(current_name);
        widgets.confirm_button.set_label(match mode {
            Mode::Save => "Save",
            Mode::Folder => "Select",
            _ => "Open",
        });
        self.search.clear();
        widgets.search_entry.set_text("");

        self.populate(widgets);
        surface_anim::reveal(
            &widgets.revealer,
            root,
            &self.config,
            AnimSurface::FileChooser,
        );
    }

    /// Reads the active directory and repaints the breadcrumb + list, applying
    /// the active search query (case-insensitive substring on the name).
    fn populate(&mut self, widgets: &FileChooserWidgets) {
        let show_hidden = self.show_hidden;
        let query = self.search.to_lowercase();
        let input = self.input.clone();
        let (sort_key, sort_asc, view) = (self.sort_key, self.sort_asc, self.view);

        // View chrome: column headers + which surface is shown.
        update_sort_indicators(widgets, sort_key, sort_asc);
        let list_mode = view == ViewMode::List;
        widgets.col_header.set_visible(list_mode);
        widgets.list_overlay.set_visible(list_mode);
        widgets.grid_scroll.set_visible(!list_mode);
        widgets.view_toggle.set_icon_name(if list_mode {
            "view-grid-symbolic"
        } else {
            "view-list-symbolic"
        });

        let Some(active) = self.active.as_mut() else {
            return;
        };
        clear_list(&widgets.file_list);
        clear_flowbox(&widgets.file_grid);
        build_breadcrumb(&widgets.crumb_bar, &active.dir, &input);

        let mut entries = list_dir(
            &active.dir,
            &active.filters,
            active.active_filter,
            active.mode,
            show_hidden,
        );
        sort_entries(&mut entries, sort_key, sort_asc);
        if !query.is_empty() {
            entries.retain(|entry| entry_name(&entry.path).to_lowercase().contains(&query));
        }
        active.entries = entries;

        for entry in &active.entries {
            let name = entry_name(&entry.path);
            if list_mode {
                widgets.file_list.append(&file_row(&name, entry));
            } else {
                widgets.file_grid.insert(&grid_cell(&name, entry), -1);
            }
        }
        widgets
            .empty_label
            .set_visible(list_mode && active.entries.is_empty());
    }

    /// Navigates to an arbitrary directory (breadcrumb / place jump). Clears the
    /// active search so the new directory shows in full.
    fn goto_dir(&mut self, widgets: &FileChooserWidgets, path: PathBuf) {
        if let Some(active) = self.active.as_mut() {
            active.dir = path;
        }
        if !self.search.is_empty() {
            self.search.clear();
            widgets.search_entry.set_text("");
        }
        self.populate(widgets);
    }

    /// Sorts by `column`, toggling direction when it's already the sort column.
    fn sort_by(&mut self, widgets: &FileChooserWidgets, column: SortColumn) {
        if self.sort_key == column {
            self.sort_asc = !self.sort_asc;
        } else {
            self.sort_key = column;
            self.sort_asc = true;
        }
        self.populate(widgets);
    }

    /// Flips between the list and grid views.
    fn toggle_view(&mut self, widgets: &FileChooserWidgets) {
        self.view = match self.view {
            ViewMode::List => ViewMode::Grid,
            ViewMode::Grid => ViewMode::List,
        };
        self.populate(widgets);
    }

    /// Cancels the request (empty reply) and animates the surface away. If the
    /// Quick Look preview is open, Escape closes that first instead.
    fn cancel(&mut self, widgets: &FileChooserWidgets, root: &gtk::Window) {
        if widgets.quicklook.is_visible() {
            widgets.quicklook.set_visible(false);
            return;
        }
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

    /// Handles a dropped path: navigate into a dropped folder, or to a dropped
    /// file's parent so it's revealed.
    fn drop_path(&mut self, widgets: &FileChooserWidgets, path: PathBuf) {
        let target = if path.is_dir() {
            Some(path)
        } else {
            path.parent().map(Path::to_path_buf)
        };
        if let Some(target) = target {
            self.goto_dir(widgets, target);
        }
    }

    /// Index of the first selected entry in the active view, if any.
    fn selected_index(&self, widgets: &FileChooserWidgets) -> Option<usize> {
        let idx = match self.view {
            ViewMode::List => widgets.file_list.selected_rows().first().map(|r| r.index()),
            ViewMode::Grid => widgets
                .file_grid
                .selected_children()
                .first()
                .map(|c| c.index()),
        };
        idx.filter(|i| *i >= 0).map(|i| i as usize)
    }

    /// Toggles the Quick Look preview card for the selected entry (spacebar).
    fn toggle_quicklook(&mut self, widgets: &FileChooserWidgets) {
        if widgets.quicklook.is_visible() {
            widgets.quicklook.set_visible(false);
            return;
        }
        let Some(i) = self.selected_index(widgets) else {
            return;
        };
        let Some(entry) = self.active.as_ref().and_then(|a| a.entries.get(i)) else {
            return;
        };
        fill_preview(&widgets.ql_icon, &widgets.ql_name, &widgets.ql_info, entry);
        widgets.quicklook.set_visible(true);
    }

    /// Refreshes the side preview pane (if visible) for the current selection.
    fn refresh_preview(&self, widgets: &FileChooserWidgets) {
        if !widgets.preview_pane.is_visible() {
            return;
        }
        let entry = self
            .selected_index(widgets)
            .and_then(|i| self.active.as_ref().and_then(|a| a.entries.get(i)));
        if let Some(entry) = entry {
            fill_preview(
                &widgets.preview_icon,
                &widgets.preview_name,
                &widgets.preview_info,
                entry,
            );
        } else {
            while let Some(child) = widgets.preview_icon.first_child() {
                widgets.preview_icon.remove(&child);
            }
            widgets.preview_name.set_label("No selection");
            widgets.preview_info.set_label("");
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
            let parent = parent.to_path_buf();
            active.dir = parent;
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
                let indices: Vec<usize> = match self.view {
                    ViewMode::List => widgets
                        .file_list
                        .selected_rows()
                        .iter()
                        .filter_map(|row| usize::try_from(row.index()).ok())
                        .collect(),
                    ViewMode::Grid => widgets
                        .file_grid
                        .selected_children()
                        .iter()
                        .filter_map(|child| usize::try_from(child.index()).ok())
                        .collect(),
                };
                let selected: Vec<String> = indices
                    .iter()
                    .filter_map(|&i| active.entries.get(i))
                    .filter(|entry| !entry.is_dir)
                    .map(|entry| uri_of(&entry.path))
                    .collect();
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

/// Lists `dir` with each entry's size + mtime. Files are filtered (unless
/// picking a folder); hidden entries are skipped unless `show_hidden`. Ordering
/// is applied later by [`sort_entries`].
fn list_dir(
    dir: &Path,
    filters: &[Filter],
    active_filter: usize,
    mode: Mode,
    show_hidden: bool,
) -> Vec<Entry> {
    let Ok(read) = std::fs::read_dir(dir) else {
        return Vec::new();
    };
    let mut entries = Vec::new();
    for dir_entry in read.flatten() {
        let name = dir_entry.file_name();
        let name = name.to_string_lossy();
        if !show_hidden && name.starts_with('.') {
            continue;
        }
        let path = dir_entry.path();
        let meta = std::fs::metadata(&path).ok();
        let is_dir = meta.as_ref().is_some_and(std::fs::Metadata::is_dir);
        if !is_dir {
            if mode == Mode::Folder {
                continue;
            }
            if !matches_filter(&name, filters, active_filter) {
                continue;
            }
        }
        let size = meta.as_ref().map_or(0, std::fs::Metadata::len);
        let modified = meta.as_ref().and_then(|m| m.modified().ok());
        entries.push(Entry {
            path,
            is_dir,
            size,
            modified,
        });
    }
    entries
}

/// Sorts entries with directories first, then by the chosen column/direction.
fn sort_entries(entries: &mut [Entry], key: SortColumn, asc: bool) {
    entries.sort_by(|a, b| {
        let dirs_first = b.is_dir.cmp(&a.is_dir);
        if dirs_first != Ordering::Equal {
            return dirs_first;
        }
        let ord = match key {
            SortColumn::Name => entry_name(&a.path)
                .to_lowercase()
                .cmp(&entry_name(&b.path).to_lowercase()),
            SortColumn::Size => a.size.cmp(&b.size),
            SortColumn::Modified => a.modified.cmp(&b.modified),
        };
        if asc { ord } else { ord.reverse() }
    });
}

/// The display name of a path (final component, lossy).
fn entry_name(path: &Path) -> String {
    path.file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_default()
}

/// Whether `name` passes the active filter. No filters → match all. A filter
/// with no glob rules (only MIME, which we can't evaluate here) also matches
/// all so nothing is hidden unexpectedly.
fn matches_filter(name: &str, filters: &[Filter], active_filter: usize) -> bool {
    if filters.is_empty() {
        return true;
    }
    let Some((_, rules)) = filters.get(active_filter) else {
        return true;
    };
    let globs: Vec<&String> = rules
        .iter()
        .filter(|(kind, _)| *kind == 0)
        .map(|(_, value)| value)
        .collect();
    if globs.is_empty() {
        return true;
    }
    globs.iter().any(|value| {
        if value.as_str() == "*" {
            true
        } else if let Some(suffix) = value.strip_prefix('*') {
            name.ends_with(suffix)
        } else {
            name == value.as_str()
        }
    })
}

/// `file://` URI for a path.
fn uri_of(path: &Path) -> String {
    gio::File::for_path(path).uri().to_string()
}

/// The user's sidebar shortcuts: `$HOME` plus the XDG-style subdirs that exist.
fn user_places() -> Vec<Place> {
    let Some(home) = std::env::var_os("HOME").map(PathBuf::from) else {
        return Vec::new();
    };
    let mut places = vec![Place {
        label: "Home".to_owned(),
        path: home.clone(),
        icon: "user-home-symbolic",
    }];
    for (name, icon) in [
        ("Desktop", "user-desktop-symbolic"),
        ("Documents", "folder-documents-symbolic"),
        ("Downloads", "folder-download-symbolic"),
        ("Music", "folder-music-symbolic"),
        ("Pictures", "folder-pictures-symbolic"),
        ("Videos", "folder-videos-symbolic"),
    ] {
        let path = home.join(name);
        if path.is_dir() {
            places.push(Place {
                label: name.to_owned(),
                path,
                icon,
            });
        }
    }
    places
}

/// Builds a sidebar place row: a symbolic icon next to its label.
fn place_row(label: &str, icon: &str) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .build();
    let image = gtk::Image::from_icon_name(icon);
    image.add_css_class("file-chooser-place-icon");
    row.append(&image);
    row.append(
        &gtk::Label::builder()
            .label(label)
            .xalign(0.0)
            .ellipsize(gtk::pango::EllipsizeMode::End)
            .build(),
    );
    row
}

/// Builds a list-view row: name column (icon/thumbnail + label), then size and
/// modified columns aligned with the clickable headers.
fn file_row(name: &str, entry: &Entry) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .build();

    let name_col = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(10)
        .hexpand(true)
        .build();
    let image = entry_icon_widget(name, &entry.path, entry.is_dir, 24);
    image.add_css_class("file-chooser-icon");
    name_col.append(&image);
    name_col.append(
        &gtk::Label::builder()
            .label(name)
            .xalign(0.0)
            .hexpand(true)
            .ellipsize(gtk::pango::EllipsizeMode::End)
            .build(),
    );
    row.append(&name_col);

    let size = gtk::Label::builder()
        .label(if entry.is_dir {
            String::from("--")
        } else {
            human_size(entry.size)
        })
        .xalign(1.0)
        .width_request(90)
        .build();
    size.add_css_class("file-chooser-cell");
    row.append(&size);

    let modified = gtk::Label::builder()
        .label(format_time(entry.modified))
        .xalign(1.0)
        .width_request(150)
        .build();
    modified.add_css_class("file-chooser-cell");
    row.append(&modified);

    row
}

/// Builds a grid-view cell: a large icon/thumbnail above a centered name.
fn grid_cell(name: &str, entry: &Entry) -> gtk::Box {
    let cell = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(6)
        .width_request(104)
        .halign(gtk::Align::Center)
        .build();
    cell.add_css_class("file-chooser-grid-cell");
    let image = entry_icon_widget(name, &entry.path, entry.is_dir, 48);
    image.add_css_class("file-chooser-grid-icon");
    image.set_halign(gtk::Align::Center);
    cell.append(&image);
    cell.append(
        &gtk::Label::builder()
            .label(name)
            .justify(gtk::Justification::Center)
            .ellipsize(gtk::pango::EllipsizeMode::End)
            .max_width_chars(14)
            .lines(2)
            .wrap(true)
            .build(),
    );
    cell
}

/// Human-readable file size (e.g. `1.2 MB`).
fn human_size(bytes: u64) -> String {
    const UNITS: [&str; 5] = ["B", "KB", "MB", "GB", "TB"];
    let mut size = bytes as f64;
    let mut unit = 0;
    while size >= 1024.0 && unit < UNITS.len() - 1 {
        size /= 1024.0;
        unit += 1;
    }
    if unit == 0 {
        format!("{bytes} B")
    } else {
        format!("{size:.1} {}", UNITS[unit])
    }
}

/// Formats a modification time as `YYYY-MM-DD HH:MM` in local time.
fn format_time(time: Option<SystemTime>) -> String {
    let Some(time) = time else {
        return String::new();
    };
    let Ok(since) = time.duration_since(SystemTime::UNIX_EPOCH) else {
        return String::new();
    };
    let Ok(datetime) = gtk::glib::DateTime::from_unix_local(since.as_secs() as i64) else {
        return String::new();
    };
    datetime
        .format("%Y-%m-%d %H:%M")
        .map(|s| s.to_string())
        .unwrap_or_default()
}

/// Updates the column header labels with the active sort arrow.
fn update_sort_indicators(widgets: &FileChooserWidgets, key: SortColumn, asc: bool) {
    let arrow = |active: bool| {
        if !active {
            ""
        } else if asc {
            " \u{2191}"
        } else {
            " \u{2193}"
        }
    };
    widgets
        .sort_name_btn
        .set_label(&format!("Name{}", arrow(key == SortColumn::Name)));
    widgets
        .sort_size_btn
        .set_label(&format!("Size{}", arrow(key == SortColumn::Size)));
    widgets
        .sort_modified_btn
        .set_label(&format!("Modified{}", arrow(key == SortColumn::Modified)));
}

/// Removes all children from a flow box.
fn clear_flowbox(flowbox: &gtk::FlowBox) {
    while let Some(child) = flowbox.first_child() {
        flowbox.remove(&child);
    }
}

/// The visual for an entry: a `gtk::Picture` showing a real thumbnail for image
/// files (full-colour, unlike `gtk::Image` which is meant for icons), else a
/// `gtk::Image` with the folder glyph or themed MIME icon.
fn entry_icon_widget(name: &str, path: &Path, is_dir: bool, size: i32) -> gtk::Widget {
    if !is_dir {
        // Detect images by extension (robust without a shared-MIME database).
        if is_raster_image(path)
            && let Some(texture) = load_texture(path, size)
        {
            let picture = gtk::Picture::for_paintable(&texture);
            picture.set_size_request(size, size);
            picture.set_content_fit(gtk::ContentFit::Cover);
            picture.set_halign(gtk::Align::Center);
            picture.set_valign(gtk::Align::Center);
            picture.add_css_class("file-chooser-thumb");
            return picture.upcast();
        }
        let (content_type, _) = gio::content_type_guess(Some(name), &[] as &[u8]);
        let image = gtk::Image::from_gicon(&gio::content_type_get_icon(&content_type));
        image.set_pixel_size(size);
        return image.upcast();
    }
    let image = gtk::Image::from_icon_name("folder-symbolic");
    image.set_pixel_size(size);
    image.upcast()
}

/// Whether the path looks like a raster image the `image` crate can decode.
fn is_raster_image(path: &Path) -> bool {
    matches!(
        path.extension()
            .and_then(|e| e.to_str())
            .map(str::to_ascii_lowercase)
            .as_deref(),
        Some(
            "png"
                | "jpg"
                | "jpeg"
                | "gif"
                | "webp"
                | "bmp"
                | "tiff"
                | "tif"
                | "ico"
                | "avif"
                | "qoi"
        )
    )
}

/// Fills a preview area (icon box + name + info labels) with an entry's details.
/// Shared by Quick Look and the side preview pane.
fn fill_preview(
    icon_box: &gtk::Box,
    name_label: &gtk::Label,
    info_label: &gtk::Label,
    entry: &Entry,
) {
    let name = entry_name(&entry.path);
    while let Some(child) = icon_box.first_child() {
        icon_box.remove(&child);
    }
    icon_box.append(&entry_icon_widget(&name, &entry.path, entry.is_dir, 96));
    name_label.set_label(&name);
    let info = if entry.is_dir {
        format!("Folder · {}", format_time(entry.modified))
    } else {
        // Type from extension (robust without a shared-MIME database).
        let kind = entry
            .path
            .extension()
            .and_then(|e| e.to_str())
            .map(|e| format!("{} file", e.to_uppercase()))
            .unwrap_or_else(|| "File".to_owned());
        format!(
            "{} · {} · {}",
            kind,
            human_size(entry.size),
            format_time(entry.modified)
        )
    };
    info_label.set_label(&info);
}

/// Loads an image file as a downscaled CPU-side `gdk::MemoryTexture` (same path
/// the wallpaper uses, so it composites under software rendering too). Files
/// above a size cap are skipped to keep directory listing snappy.
fn load_texture(path: &Path, size: i32) -> Option<gtk::gdk::Texture> {
    const MAX_BYTES: u64 = 32 * 1024 * 1024;
    if std::fs::metadata(path).ok()?.len() > MAX_BYTES {
        return None;
    }
    // Decode and shrink to ~2x the display size for crisp thumbnails.
    let target = (size.max(1) as u32) * 2;
    let rgba = image::open(path).ok()?.thumbnail(target, target).to_rgba8();
    let (width, height) = (rgba.width() as i32, rgba.height() as i32);
    let stride = (rgba.width() * 4) as usize;
    let bytes = gtk::glib::Bytes::from_owned(rgba.into_raw());
    let texture = gtk::gdk::MemoryTexture::new(
        width,
        height,
        gtk::gdk::MemoryFormat::R8g8b8a8,
        &bytes,
        stride,
    );
    Some(texture.upcast())
}

/// Sidebar "Locations": the filesystem root plus currently-mounted volumes.
fn other_locations() -> Vec<Place> {
    let mut locations = vec![Place {
        label: "Computer".to_owned(),
        path: PathBuf::from("/"),
        icon: "drive-harddisk-symbolic",
    }];
    let monitor = gio::VolumeMonitor::get();
    for mount in monitor.mounts() {
        if mount.is_shadowed() {
            continue;
        }
        let Some(root) = mount.root().path() else {
            continue;
        };
        locations.push(Place {
            label: mount.name().to_string(),
            path: root,
            icon: "drive-removable-media-symbolic",
        });
    }
    locations
}

/// Rebuilds the breadcrumb bar as clickable ancestor buttons. `$HOME` collapses
/// to a single "Home" crumb; otherwise the trail starts at the filesystem root.
fn build_breadcrumb(bar: &gtk::Box, dir: &Path, input: &Sender<FileChooserInput>) {
    while let Some(child) = bar.first_child() {
        bar.remove(&child);
    }

    let home = std::env::var_os("HOME").map(PathBuf::from);
    let (mut acc, base_label, tail) = if let Some(home) = &home
        && let Ok(rest) = dir.strip_prefix(home)
    {
        (home.clone(), "Home".to_owned(), rest.to_path_buf())
    } else {
        let rest = dir.strip_prefix("/").unwrap_or(dir).to_path_buf();
        (PathBuf::from("/"), "/".to_owned(), rest)
    };

    let components: Vec<PathComponent> = tail.components().collect();
    append_crumb(bar, &base_label, &acc, input, components.is_empty());
    for (i, component) in components.iter().enumerate() {
        let PathComponent::Normal(name) = component else {
            continue;
        };
        acc.push(name);
        bar.append(&crumb_separator());
        append_crumb(
            bar,
            &name.to_string_lossy(),
            &acc,
            input,
            i + 1 == components.len(),
        );
    }
}

/// Appends one breadcrumb button targeting `target`.
fn append_crumb(
    bar: &gtk::Box,
    label: &str,
    target: &Path,
    input: &Sender<FileChooserInput>,
    is_current: bool,
) {
    let button = gtk::Button::builder().label(label).build();
    button.add_css_class("file-chooser-crumb");
    button.add_css_class("flat");
    if is_current {
        button.add_css_class("current");
    }
    button.set_cursor_from_name(Some("pointer"));
    let target = target.to_path_buf();
    let input = input.clone();
    button.connect_clicked(move |_| input.emit(FileChooserInput::Crumb(target.clone())));
    bar.append(&button);
}

/// A `›` separator label between breadcrumb buttons.
fn crumb_separator() -> gtk::Label {
    let sep = gtk::Label::new(Some("›"));
    sep.add_css_class("file-chooser-crumb-sep");
    sep
}

/// Removes all rows from a list box.
fn clear_list(list: &gtk::ListBox) {
    while let Some(child) = list.first_child() {
        list.remove(&child);
    }
}

/// Installs the window's keyboard shortcuts (Escape cancels, spacebar Quick
/// Look) and the drag-and-drop target (drop a path to navigate).
fn install_controllers(
    root: &gtk::Window,
    revealer: &gtk::Revealer,
    search: &gtk::SearchEntry,
    input: &Sender<FileChooserInput>,
) {
    // Capture phase so Escape fires before a focused child swallows it.
    let key = gtk::EventControllerKey::new();
    key.set_propagation_phase(gtk::PropagationPhase::Capture);
    key.connect_key_pressed({
        let input = input.clone();
        let search = search.clone();
        move |_, keyval, _, _| {
            if keyval == gtk::gdk::Key::Escape {
                input.emit(FileChooserInput::Cancel);
                gtk::glib::Propagation::Stop
            } else if keyval == gtk::gdk::Key::space && !search.has_focus() {
                // Spacebar Quick Look — but only when not typing in search.
                input.emit(FileChooserInput::ToggleQuickLook);
                gtk::glib::Propagation::Stop
            } else {
                gtk::glib::Propagation::Proceed
            }
        }
    });
    root.add_controller(key);

    // Drag a file/folder onto the dialog to navigate there (like Finder).
    let drop = gtk::DropTarget::new(
        gtk::gdk::FileList::static_type(),
        gtk::gdk::DragAction::COPY,
    );
    drop.connect_drop({
        let input = input.clone();
        move |_, value, _, _| {
            if let Ok(list) = value.get::<gtk::gdk::FileList>()
                && let Some(path) = list.files().first().and_then(gtk::gio::File::path)
            {
                input.emit(FileChooserInput::Dropped(path));
                return true;
            }
            false
        }
    });
    revealer.add_controller(drop);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn entry(name: &str, is_dir: bool, size: u64) -> Entry {
        Entry {
            path: PathBuf::from(name),
            is_dir,
            size,
            modified: None,
        }
    }

    fn names(entries: &[Entry]) -> Vec<String> {
        entries.iter().map(|e| entry_name(&e.path)).collect()
    }

    #[test]
    fn human_size_scales_by_unit() {
        assert_eq!(human_size(0), "0 B");
        assert_eq!(human_size(512), "512 B");
        assert_eq!(human_size(1024), "1.0 KB");
        assert_eq!(human_size(1536), "1.5 KB");
        assert_eq!(human_size(1024 * 1024), "1.0 MB");
        assert_eq!(human_size(3 * 1024 * 1024 * 1024), "3.0 GB");
    }

    #[test]
    fn matches_filter_handles_globs_mime_and_bounds() {
        let filters = vec![
            (
                "Images".to_owned(),
                vec![(0u32, "*.png".to_owned()), (0, "*.jpg".to_owned())],
            ),
            ("Text".to_owned(), vec![(0, "*.txt".to_owned())]),
        ];
        // Active filter 0 (Images): png/jpg pass, txt fails.
        assert!(matches_filter("a.png", &filters, 0));
        assert!(matches_filter("a.jpg", &filters, 0));
        assert!(!matches_filter("a.txt", &filters, 0));
        // Active filter 1 (Text): only txt.
        assert!(matches_filter("a.txt", &filters, 1));
        assert!(!matches_filter("a.png", &filters, 1));
        // No filters → match all.
        assert!(matches_filter("anything", &[], 0));
        // `*` matches all; exact names are case-sensitive.
        let star = vec![("All".to_owned(), vec![(0u32, "*".to_owned())])];
        assert!(matches_filter("x.bin", &star, 0));
        let exact = vec![("Make".to_owned(), vec![(0u32, "Makefile".to_owned())])];
        assert!(matches_filter("Makefile", &exact, 0));
        assert!(!matches_filter("makefile", &exact, 0));
        // MIME-only filter (no glob rules) → match all so nothing is hidden.
        let mime = vec![("Img".to_owned(), vec![(1u32, "image/png".to_owned())])];
        assert!(matches_filter("whatever.xyz", &mime, 0));
        // Out-of-range active filter → match all.
        assert!(matches_filter("x", &filters, 99));
    }

    #[test]
    fn sort_entries_keeps_dirs_first() {
        let mut entries = vec![
            entry("b.txt", false, 10),
            entry("Adir", true, 0),
            entry("a.txt", false, 30),
            entry("Zdir", true, 0),
        ];

        sort_entries(&mut entries, SortColumn::Name, true);
        assert_eq!(names(&entries), ["Adir", "Zdir", "a.txt", "b.txt"]);

        sort_entries(&mut entries, SortColumn::Name, false);
        assert_eq!(names(&entries), ["Zdir", "Adir", "b.txt", "a.txt"]);

        // Size sort: dirs stay first, files ordered by size ascending.
        sort_entries(&mut entries, SortColumn::Size, true);
        let n = names(&entries);
        assert!(entries[0].is_dir && entries[1].is_dir);
        assert_eq!(&n[2..], ["b.txt", "a.txt"]);
    }

    #[test]
    fn entry_name_is_basename() {
        assert_eq!(entry_name(Path::new("/foo/bar/baz.txt")), "baz.txt");
        assert_eq!(entry_name(Path::new("solo")), "solo");
    }

    #[test]
    fn detects_raster_images_by_extension() {
        assert!(is_raster_image(Path::new("/x/a.png")));
        assert!(is_raster_image(Path::new("/x/a.JPG")));
        assert!(is_raster_image(Path::new("/x/photo.jpeg")));
        assert!(is_raster_image(Path::new("/x/b.webp")));
        assert!(!is_raster_image(Path::new("/x/a.txt")));
        assert!(!is_raster_image(Path::new("/x/a.svg"))); // not a raster format we decode
        assert!(!is_raster_image(Path::new("/x/noext")));
    }

    #[test]
    fn thumbnail_decode_pipeline_produces_rgba() {
        // Exercises the exact decode `load_texture` performs (image::open ->
        // thumbnail -> to_rgba8) on a real PNG, so the thumbnail data path is
        // verified without needing a display/GPU.
        let path = std::env::temp_dir().join("wayle_fc_thumb_test.png");
        let mut src = image::RgbaImage::new(16, 8);
        for (x, _y, px) in src.enumerate_pixels_mut() {
            *px = if x < 8 {
                image::Rgba([200, 30, 40, 255])
            } else {
                image::Rgba([20, 60, 220, 255])
            };
        }
        src.save(&path).expect("write test png");

        let target = 24u32 * 2;
        let rgba = image::open(&path)
            .expect("open png")
            .thumbnail(target, target)
            .to_rgba8();
        std::fs::remove_file(&path).ok();

        // Non-empty, fits within the target box, RGBA8 (4 bytes/px), and the
        // left half is reddish (decode preserved real pixels, not blank).
        assert!(rgba.width() > 0 && rgba.height() > 0);
        assert!(rgba.width() <= target && rgba.height() <= target);
        assert_eq!(
            rgba.as_raw().len(),
            (rgba.width() * rgba.height() * 4) as usize
        );
        let left = rgba.get_pixel(0, 0);
        assert!(
            left[0] > left[2],
            "left half should be reddish, got {left:?}"
        );
    }
}
