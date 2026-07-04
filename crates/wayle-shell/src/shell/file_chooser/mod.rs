//! File chooser — a custom animated layer-shell file browser.
//!
//! Replaces the native `gtk::FileDialog` with our own surface so the portal
//! file picker animates congruently (`AnimSurface::FileChooser`) like the rest
//! of the shell. Backs `com.wayle.FileChooser1`: open file(s) / pick a folder /
//! save, with the portal's filters + starting folder. Returns `file://` URIs.

use std::{
    cmp::Ordering,
    path::{Component as PathComponent, Path, PathBuf},
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering as AtomicOrdering},
    },
    time::SystemTime,
};

use gtk4_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};
use relm4::{
    Sender, gtk,
    gtk::{gio, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;
use wayle_config::{ConfigService, schemas::animations::AnimSurface};
use wayle_widgets::prelude::WayleRevealer;

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
    /// Internal: a recursive search finished — `paths` matched `query` under the
    /// search root (delivered async; dropped if the query has since changed).
    SearchResults { query: String, paths: Vec<PathBuf> },
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
    /// Internal: a column divider was dragged — re-lay the rows to match.
    ColumnsResized,
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
            Self::SearchResults { query, paths } => f
                .debug_struct("SearchResults")
                .field("query", query)
                .field("matches", &paths.len())
                .finish(),
            Self::Crumb(p) => f.debug_tuple("Crumb").field(p).finish(),
            Self::GoUp => f.write_str("GoUp"),
            Self::ToggleHidden => f.write_str("ToggleHidden"),
            Self::SelectFilter(i) => f.debug_tuple("SelectFilter").field(i).finish(),
            Self::Sort(_) => f.write_str("Sort"),
            Self::ToggleView => f.write_str("ToggleView"),
            Self::ToggleQuickLook => f.write_str("ToggleQuickLook"),
            Self::TogglePreview => f.write_str("TogglePreview"),
            Self::SelectionChanged => f.write_str("SelectionChanged"),
            Self::ColumnsResized => f.write_str("ColumnsResized"),
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
#[derive(Clone)]
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
    Kind,
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
    /// Cached results of the active recursive search (empty when not searching),
    /// kept so re-sorting / view toggles don't re-walk the tree.
    search_entries: Vec<Entry>,
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
    size_col_w: i32,
    mod_col_w: i32,
    kind_col_w: i32,
    // Sheet geometry (size/position) lives on the widgets during a drag, not on
    // the model — the resize/move gestures mutate the surface + window directly
    // so dragging stays smooth (no per-event message round-trip).
    /// Monotonic search generation. Bumped on every query change; the recursive
    /// walk captures its generation and bails the moment a newer keystroke
    /// supersedes it, so superseded walks don't keep pegging CPU/IO behind the
    /// debounce.
    search_gen: Arc<AtomicU64>,
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
            WayleRevealer {
                set_reveal_child: false,

                #[name = "overlay"]
                gtk::Overlay {
                #[name = "surface"]
                gtk::Box {
                    add_css_class: "file-chooser-surface",
                    set_orientation: gtk::Orientation::Vertical,
                    set_spacing: 0,
                    set_width_request: 760,
                    set_height_request: 520,

                    // --- Header: nav + centered title + hidden toggle ---
                    // Doubles as the drag handle to move the sheet (like a titlebar).
                    #[name = "header"]
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
                                set_tooltip_text: Some("Up"),
                                connect_clicked => FileChooserInput::GoUp,
                                // Explicit centered image child — `set_icon_name`'s
                                // internal image sat top-left in the square button;
                                // GTK centres a child whose align is Center.
                                #[wrap(Some)]
                                set_child = &gtk::Image {
                                    set_icon_name: Some("go-up-symbolic"),
                                    set_halign: gtk::Align::Center,
                                    set_valign: gtk::Align::Center,
                                },
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
                            set_placeholder_text: Some("Search this folder"),
                            set_width_request: 150,
                            set_valign: gtk::Align::Center,
                            // Debounce: only emit `search-changed` after typing
                            // pauses, so a recursive walk fires once per pause
                            // instead of once per keystroke.
                            set_search_delay: 300,
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
                                    // None: places are navigation buttons, not a
                                    // selection — Single left the clicked row
                                    // painted as "active" indefinitely.
                                    set_selection_mode: gtk::SelectionMode::None,
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
                                    set_selection_mode: gtk::SelectionMode::None,
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
                                // The resize grips below set `vexpand: true` so
                                // they're tall enough to grab. In GTK4 a child's
                                // vexpand propagates up, which would make this
                                // header claim half the vertical space (a tall
                                // band with the labels floating mid-height).
                                // Pin it off so the header hugs its text and the
                                // list takes the slack; the grips still fill this
                                // (now short) header height.
                                set_vexpand: false,
                                #[name = "sort_name_btn"]
                                gtk::Button {
                                    add_css_class: "file-chooser-col",
                                    add_css_class: "flat",
                                    set_hexpand: true,
                                    set_label: "Name",
                                    connect_clicked => FileChooserInput::Sort(SortColumn::Name),
                                },
                                #[name = "size_handle"]
                                gtk::Box {
                                    add_css_class: "file-chooser-col-grip",
                                    set_vexpand: true,
                                },
                                #[name = "sort_size_btn"]
                                gtk::Button {
                                    add_css_class: "file-chooser-col",
                                    add_css_class: "flat",
                                    set_width_request: 90,
                                    set_label: "Size",
                                    connect_clicked => FileChooserInput::Sort(SortColumn::Size),
                                },
                                #[name = "mod_handle"]
                                gtk::Box {
                                    add_css_class: "file-chooser-col-grip",
                                    set_vexpand: true,
                                },
                                #[name = "sort_modified_btn"]
                                gtk::Button {
                                    add_css_class: "file-chooser-col",
                                    add_css_class: "flat",
                                    set_width_request: 150,
                                    set_label: "Modified",
                                    connect_clicked => FileChooserInput::Sort(SortColumn::Modified),
                                },
                                #[name = "kind_handle"]
                                gtk::Box {
                                    add_css_class: "file-chooser-col-grip",
                                    set_vexpand: true,
                                },
                                #[name = "sort_kind_btn"]
                                gtk::Button {
                                    add_css_class: "file-chooser-col",
                                    add_css_class: "flat",
                                    set_width_request: 90,
                                    set_label: "Kind",
                                    connect_clicked => FileChooserInput::Sort(SortColumn::Kind),
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
                                        // Single click selects (ctrl/shift extend in
                                        // multi mode); double click activates (open /
                                        // descend). Without this single click would
                                        // both select and open, fighting multiselect.
                                        set_activate_on_single_click: false,
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
                                    set_activate_on_single_click: false,
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
                        // File-type filter: our own toggle + in-surface popup
                        // (below) instead of a GtkDropDown — its popover is an
                        // xdg_popup, which doesn't reliably receive pointer
                        // input over a keyboard-exclusive layer surface.
                        #[name = "filter_button"]
                        gtk::ToggleButton {
                            add_css_class: "file-chooser-filter",
                            set_valign: gtk::Align::Center,
                            set_visible: false,
                            #[wrap(Some)]
                            #[name = "filter_button_label"]
                            set_child = &gtk::Label {
                                set_xalign: 0.0,
                                set_ellipsize: gtk::pango::EllipsizeMode::End,
                                set_max_width_chars: 28,
                            },
                        },
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
                // File-type filter popup — an overlay child of the sheet itself
                // (not an xdg_popup), so it always gets pointer input.
                #[name = "filter_popup"]
                add_overlay = &gtk::Box {
                    add_css_class: "file-chooser-filter-popup",
                    set_orientation: gtk::Orientation::Vertical,
                    set_halign: gtk::Align::Start,
                    set_valign: gtk::Align::End,
                    set_margin_start: 16,
                    set_margin_bottom: 56,
                    set_visible: false,
                    gtk::ScrolledWindow {
                        set_hscrollbar_policy: gtk::PolicyType::Never,
                        // Both: without natural-width propagation the scrolled
                        // window shrinks the ellipsized labels to bare "…".
                        set_propagate_natural_width: true,
                        set_propagate_natural_height: true,
                        set_max_content_height: 320,
                        #[name = "filter_list"]
                        gtk::ListBox {
                            add_css_class: "file-chooser-filter-list",
                            set_selection_mode: gtk::SelectionMode::None,
                        },
                    },
                },
                // Bottom-right corner grip — drag to resize the whole sheet
                // (layer-shell has no window decorations, so we provide our own).
                #[name = "resize_grip"]
                add_overlay = &gtk::Box {
                    add_css_class: "file-chooser-resize-grip",
                    set_halign: gtk::Align::End,
                    set_valign: gtk::Align::End,
                    set_width_request: 18,
                    set_height_request: 18,
                },
                }
            }
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let (sort_key, sort_asc, view) = load_ui_state();
        let model = FileChooser {
            config: init,
            active: None,
            places: user_places(),
            locations: other_locations(),
            show_hidden: false,
            search: String::new(),
            sort_key,
            sort_asc,
            view,
            size_col_w: 90,
            mod_col_w: 150,
            kind_col_w: 90,
            search_gen: Arc::new(AtomicU64::new(0)),
            input: sender.input_sender().clone(),
        };
        let widgets = view_output!();

        root.init_layer_shell();
        root.set_namespace(Some("wayle-file-chooser"));
        root.set_layer(Layer::Overlay);
        // Pin the default size below any real sheet size so the window always
        // tracks the surface's size *request*. Without this GTK keeps the
        // largest allocation it has seen: the corner-drag could grow the sheet
        // but never shrink it back.
        root.set_default_size(1, 1);
        // Grab keyboard while shown so type-ahead, Esc, and arrow navigation work
        // immediately — like a native modal picker.
        root.set_keyboard_mode(KeyboardMode::Exclusive);
        root.set_exclusive_zone(-1);
        // Anchor top-left and position via margins so the header drag can move the
        // sheet; seed the margins to a centred spot from the monitor geometry.
        root.set_anchor(Edge::Top, true);
        root.set_anchor(Edge::Left, true);
        let (cx, cy) = center_margins(760, 520);
        root.set_margin(Edge::Left, cx);
        root.set_margin(Edge::Top, cy);

        for place in &model.places {
            widgets
                .places_list
                .append(&place_row(&place.label, place.icon));
        }
        for place in &model.locations {
            widgets
                .locations_list
                .append(&place_row(&place.label, place.icon));
        }
        wire_widget_signals(&widgets, sender.input_sender());

        // Hand cursor on every interactive element (GTK ignores CSS `cursor`).
        for widget in [
            widgets.up_button.upcast_ref::<gtk::Widget>(),
            widgets.hidden_toggle.upcast_ref(),
            widgets.filter_button.upcast_ref(),
            widgets.filter_list.upcast_ref(),
            widgets.cancel_button.upcast_ref(),
            widgets.confirm_button.upcast_ref(),
            widgets.view_toggle.upcast_ref(),
            widgets.preview_toggle.upcast_ref(),
            widgets.sort_name_btn.upcast_ref(),
            widgets.sort_size_btn.upcast_ref(),
            widgets.sort_modified_btn.upcast_ref(),
            widgets.sort_kind_btn.upcast_ref(),
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

        install_column_resize(&widgets, sender.input_sender(), &root);
        setup_surface_move(&widgets.header, &root);
        widgets.filter_button.connect_toggled({
            let popup = widgets.filter_popup.clone();
            move |button| popup.set_visible(button.is_active())
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
            } => self.begin(
                widgets,
                root,
                &title,
                Mode::Save,
                filters,
                &current_folder,
                &current_name,
                reply,
            ),
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
            FileChooserInput::Search(query) => self.set_search(widgets, query),
            FileChooserInput::SearchResults { query, paths } => {
                self.apply_search_results(widgets, &query, paths);
            }
            FileChooserInput::Crumb(path) => self.goto_dir(widgets, path),
            FileChooserInput::GoUp => self.go_up(widgets),
            FileChooserInput::ToggleHidden => {
                self.show_hidden = widgets.hidden_toggle.is_active();
                self.refresh_view(widgets);
            }
            FileChooserInput::SelectFilter(index) => {
                if let Some(active) = self.active.as_mut() {
                    active.active_filter = index as usize;
                    if let Some(filter) = active.filters.get(index as usize) {
                        widgets.filter_button_label.set_label(&filter_label(filter));
                    }
                }
                widgets.filter_button.set_active(false);
                self.refresh_view(widgets);
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
            FileChooserInput::ColumnsResized => {
                self.size_col_w = widgets.sort_size_btn.width_request().max(50);
                self.mod_col_w = widgets.sort_modified_btn.width_request().max(50);
                self.kind_col_w = widgets.sort_kind_btn.width_request().max(50);
                self.populate(widgets);
            }
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

        // Populate the filter dropdown from the request's named filters. Append
        // an "All Files" escape hatch (unless a catch-all is already offered) so
        // the user can always bypass a restrictive app-supplied filter.
        let mut filters = filters;
        if !filters.is_empty() && !filters.iter().any(matches_everything) {
            filters.push(("All Files".to_owned(), vec![(0, "*".to_owned())]));
        }
        widgets.filter_button.set_active(false);
        widgets.filter_button.set_visible(!filters.is_empty());
        clear_list(&widgets.filter_list);
        for filter in &filters {
            widgets
                .filter_list
                .append(&filter_row(&filter_label(filter)));
        }
        if let Some(first) = filters.first() {
            widgets.filter_button_label.set_label(&filter_label(first));
        }

        self.active = Some(Active {
            mode,
            dir,
            filters,
            active_filter: 0,
            entries: Vec::new(),
            search_entries: Vec::new(),
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

    /// Repaints the breadcrumb + list. When a search is active the rows come
    /// from the recursive results ([`Self::apply_search_results`]) and are
    /// labelled by their path relative to the search root; otherwise the current
    /// directory is listed in full.
    fn populate(&mut self, widgets: &FileChooserWidgets) {
        let show_hidden = self.show_hidden;
        let searching = !self.search.is_empty();
        let query = self.search.clone();
        let input = self.input.clone();
        let (sort_key, sort_asc, view) = (self.sort_key, self.sort_asc, self.view);
        let (size_w, mod_w, kind_w) = (self.size_col_w, self.mod_col_w, self.kind_col_w);

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

        let root = active.dir.clone();
        let mut entries = if searching {
            active.search_entries.clone()
        } else {
            list_dir(
                &active.dir,
                &active.filters,
                active.active_filter,
                active.mode,
                show_hidden,
            )
        };
        // Searching ranks by relevance (closest matches first) rather than the
        // column sort; a plain listing honours the clicked column.
        if searching {
            sort_search_entries(&mut entries, &root, &query);
        } else {
            sort_entries(&mut entries, sort_key, sort_asc);
        }
        active.entries = entries;

        for entry in &active.entries {
            // In search mode show the path relative to the root for context;
            // in a plain listing the basename is the whole story.
            let name = if searching {
                entry.path.strip_prefix(&root).map_or_else(
                    |_| entry_name(&entry.path),
                    |rel| rel.to_string_lossy().into_owned(),
                )
            } else {
                entry_name(&entry.path)
            };
            if list_mode {
                widgets
                    .file_list
                    .append(&file_row(&name, entry, size_w, mod_w, kind_w, searching));
            } else {
                widgets.file_grid.insert(&grid_cell(&name, entry), -1);
            }
        }
        widgets
            .empty_label
            .set_visible(list_mode && active.entries.is_empty());
    }

    /// Handles a search-query change: empty clears search mode and re-lists the
    /// directory; non-empty kicks off a recursive walk from the current folder
    /// (off the UI thread) whose result arrives as [`FileChooserInput::SearchResults`].
    ///
    /// Every call bumps the search generation; the spawned walk carries that
    /// generation and aborts as soon as a newer query supersedes it, so the
    /// blocking pool isn't left grinding through stale full-tree walks (the
    /// freeze). The `SearchEntry`'s `search-delay` already collapses bursts of
    /// keystrokes into one call, so in steady state only the final query walks.
    fn set_search(&mut self, widgets: &FileChooserWidgets, query: String) {
        self.search = query;
        // Supersede any in-flight walk regardless of empty/non-empty.
        let generation = self.search_gen.fetch_add(1, AtomicOrdering::SeqCst) + 1;
        if self.search.is_empty() {
            if let Some(active) = self.active.as_mut() {
                active.search_entries.clear();
            }
            self.populate(widgets);
            return;
        }
        let Some(active) = self.active.as_ref() else {
            return;
        };
        let root = active.dir.clone();
        let query = self.search.clone();
        let show_hidden = self.show_hidden;
        let input = self.input.clone();
        let cancel = self.search_gen.clone();
        relm4::spawn(async move {
            let walk_query = query.clone();
            let paths = tokio::task::spawn_blocking(move || {
                walk_search(&root, &walk_query, show_hidden, &cancel, generation)
            })
            .await
            .unwrap_or_default();
            let _ = input.send(FileChooserInput::SearchResults { query, paths });
        });
    }

    /// Repaints, re-running the recursive walk first when a search is active
    /// (since hidden-files / filter changes invalidate the cached results).
    fn refresh_view(&mut self, widgets: &FileChooserWidgets) {
        if self.search.is_empty() {
            self.populate(widgets);
        } else {
            self.set_search(widgets, self.search.clone());
        }
    }

    /// Applies async recursive-search results, ignoring them if the query has
    /// moved on. Builds entries (stat + filter/mode rules) and repaints.
    fn apply_search_results(
        &mut self,
        widgets: &FileChooserWidgets,
        query: &str,
        paths: Vec<PathBuf>,
    ) {
        if query != self.search {
            return;
        }
        let Some(active) = self.active.as_mut() else {
            return;
        };
        active.search_entries =
            build_search_entries(paths, &active.filters, active.active_filter, active.mode);
        self.populate(widgets);
    }

    /// Navigates to an arbitrary directory (breadcrumb / place jump). Clears the
    /// active search so the new directory shows in full.
    fn goto_dir(&mut self, widgets: &FileChooserWidgets, path: PathBuf) {
        if let Some(active) = self.active.as_mut() {
            active.dir = path;
            active.search_entries.clear();
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
        save_ui_state(self.sort_key, self.sort_asc, self.view);
        self.populate(widgets);
    }

    /// Flips between the list and grid views.
    fn toggle_view(&mut self, widgets: &FileChooserWidgets) {
        self.view = match self.view {
            ViewMode::List => ViewMode::Grid,
            ViewMode::Grid => ViewMode::List,
        };
        save_ui_state(self.sort_key, self.sort_asc, self.view);
        self.populate(widgets);
    }

    /// Cancels the request (empty reply) and animates the surface away. If the
    /// filter popup or Quick Look preview is open, Escape closes that first.
    fn cancel(&mut self, widgets: &FileChooserWidgets, root: &gtk::Window) {
        if widgets.filter_button.is_active() {
            widgets.filter_button.set_active(false);
            return;
        }
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

    /// Handles a row activation (double-click / Enter): descend into dirs, seed
    /// the Save name, or open the file(s) in open modes. Single click only
    /// selects (so ctrl/shift multiselect works); activation is the explicit
    /// "open this" gesture.
    fn activate(&mut self, widgets: &FileChooserWidgets, index: u32) {
        let Some(active) = self.active.as_ref() else {
            return;
        };
        let Some(entry) = active.entries.get(index as usize) else {
            return;
        };
        let is_dir = entry.is_dir;
        let mode = active.mode;
        let path = entry.path.clone();
        if is_dir {
            // goto_dir clears any active search so the opened directory lists in
            // full (search results may have come from elsewhere in the tree).
            self.goto_dir(widgets, path);
        } else if mode == Mode::Save {
            if let Some(name) = path.file_name() {
                widgets.name_entry.set_text(&name.to_string_lossy());
            }
        } else {
            // Open mode: double-clicking a file opens it (the activated row is
            // selected, so Confirm picks it up — alongside any ctrl/shift set).
            self.input.emit(FileChooserInput::Confirm);
        }
    }

    fn go_up(&mut self, widgets: &FileChooserWidgets) {
        let parent = self
            .active
            .as_ref()
            .and_then(|active| active.dir.parent().map(Path::to_path_buf));
        if let Some(parent) = parent {
            self.goto_dir(widgets, parent);
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

/// Cap on recursive-search results — keeps the walk and the row build bounded
/// on huge trees (and the UI responsive).
const SEARCH_RESULT_CAP: usize = 1000;

/// Recursively walks `root` (depth-first, bounded by [`SEARCH_RESULT_CAP`]),
/// collecting every entry whose name contains `query` (case-insensitive). Honors
/// the hidden-files setting: dot-entries are skipped, and their subtrees aren't
/// descended into, unless `show_hidden`. Runs on a blocking thread.
///
/// `cancel`/`my_gen` make a superseded walk abort early: once
/// `cancel.load() != my_gen` (a newer query bumped the generation) the walk
/// stops, so the blocking pool isn't left grinding a stale full-tree scan.
fn walk_search(
    root: &Path,
    query: &str,
    show_hidden: bool,
    cancel: &AtomicU64,
    my_gen: u64,
) -> Vec<PathBuf> {
    let needle = query.to_lowercase();
    let mut out = Vec::new();
    let mut stack = vec![root.to_path_buf()];
    while let Some(dir) = stack.pop() {
        if out.len() >= SEARCH_RESULT_CAP || cancel.load(AtomicOrdering::SeqCst) != my_gen {
            break;
        }
        let Ok(read) = std::fs::read_dir(&dir) else {
            continue;
        };
        for dir_entry in read.flatten() {
            // Bail mid-directory too — a single huge directory shouldn't pin the
            // thread after the query already moved on.
            if cancel.load(AtomicOrdering::SeqCst) != my_gen {
                return out;
            }
            let name = dir_entry.file_name();
            let name = name.to_string_lossy();
            if !show_hidden && name.starts_with('.') {
                continue;
            }
            let path = dir_entry.path();
            if name.to_lowercase().contains(&needle) {
                out.push(path.clone());
                if out.len() >= SEARCH_RESULT_CAP {
                    break;
                }
            }
            if dir_entry.file_type().is_ok_and(|t| t.is_dir()) {
                stack.push(path);
            }
        }
    }
    out
}

/// Builds list entries from recursive-search result paths, applying the same
/// mode/filter rules as a normal listing (directories always shown; in `Folder`
/// mode files are dropped; otherwise files must pass the active filter).
fn build_search_entries(
    paths: Vec<PathBuf>,
    filters: &[Filter],
    active_filter: usize,
    mode: Mode,
) -> Vec<Entry> {
    let mut entries = Vec::new();
    for path in paths {
        let meta = std::fs::metadata(&path).ok();
        let is_dir = meta.as_ref().is_some_and(std::fs::Metadata::is_dir);
        if !is_dir {
            if mode == Mode::Folder {
                continue;
            }
            if !matches_filter(&entry_name(&path), filters, active_filter) {
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
            // Group by type (extension), then by name within a type.
            SortColumn::Kind => entry_kind(a)
                .to_lowercase()
                .cmp(&entry_kind(b).to_lowercase())
                .then_with(|| {
                    entry_name(&a.path)
                        .to_lowercase()
                        .cmp(&entry_name(&b.path).to_lowercase())
                }),
        };
        if asc { ord } else { ord.reverse() }
    });
}

/// The displayed "kind" of an entry: `Folder` for directories, the uppercased
/// extension (e.g. `PNG`) for files, or `File` when there's no extension.
fn entry_kind(entry: &Entry) -> String {
    if entry.is_dir {
        return "Folder".to_owned();
    }
    entry
        .path
        .extension()
        .map(|e| e.to_string_lossy().to_uppercase())
        .unwrap_or_else(|| "File".to_owned())
}

/// A relevance score for a search hit (lower = better): how well the basename
/// matches the query, then how shallow the result is under the search root.
/// Exact name beats prefix beats substring; among equal matches, shallower
/// paths win — so top-level hits sort above ones buried deep in nested subdirs.
fn search_rank(entry: &Entry, root: &Path, query_lower: &str) -> (u8, usize, usize) {
    let name = entry_name(&entry.path).to_lowercase();
    let match_class = if name == query_lower {
        0
    } else if name.starts_with(query_lower) {
        1
    } else {
        2
    };
    // Depth = path components below the root (fewer = closer to where you are).
    let depth = entry.path.strip_prefix(root).map_or_else(
        |_| entry.path.components().count(),
        |rel| rel.components().count(),
    );
    // Where the match falls in the name — earlier is tighter (e.g. "php" in
    // "php.ini" beats "php" in "my-php-helper").
    let pos = name.find(query_lower).unwrap_or(usize::MAX);
    (match_class, depth, pos)
}

/// Orders recursive-search hits by relevance: closest/shallowest, best-matching
/// names first, breaking ties by the path so the order is stable.
fn sort_search_entries(entries: &mut [Entry], root: &Path, query: &str) {
    let q = query.to_lowercase();
    entries.sort_by(|a, b| {
        search_rank(a, root, &q)
            .cmp(&search_rank(b, root, &q))
            .then_with(|| a.path.cmp(&b.path))
    });
}

/// The display name of a path (final component, lossy).
fn entry_name(path: &Path) -> String {
    path.file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_default()
}

/// Display label for a filter: its name. Apps that want patterns visible put
/// them in the name ("PNG images (*.png)"); appending the raw rules ourselves
/// rendered walls of case-insensitive globs like `*.[Pp][Nn][Gg]` and blew the
/// sheet wider than the screen. Only a nameless filter falls back to its
/// patterns (globs as-is, MIME types reverse-mapped to `*.ext` via `mime2ext`).
fn filter_label(filter: &Filter) -> String {
    if !filter.0.trim().is_empty() {
        return filter.0.clone();
    }
    let mut patterns: Vec<String> = Vec::new();
    for (kind, value) in &filter.1 {
        let token = match kind {
            1 => mime_ext(value).map_or_else(|| value.clone(), |ext| format!("*.{ext}")),
            _ => value.clone(),
        };
        if !patterns.contains(&token) {
            patterns.push(token);
        }
    }
    patterns.join(", ")
}

/// Canonical extension for a MIME type. Overrides the handful of common media
/// types where `mime2ext`'s pick is missing or an obscure alias (`audio/mpeg`
/// → `mpga`, `video/quicktime` → `qt`), so labels read `*.mp3` / `*.mov`.
/// Everything else defers to `mime2ext`; `None` (wildcards / unknown types) lets
/// the caller fall back to the raw MIME string.
fn mime_ext(mime: &str) -> Option<&'static str> {
    match mime {
        "audio/mpeg" | "audio/mp3" => Some("mp3"),
        "audio/flac" | "audio/x-flac" => Some("flac"),
        "audio/aac" => Some("aac"),
        "video/quicktime" => Some("mov"),
        other => mime2ext::mime2ext(other),
    }
}

/// Whether a filter already matches everything (a `*`/`*.*` glob), so we don't
/// append a redundant "All Files" entry.
fn matches_everything(filter: &Filter) -> bool {
    filter
        .1
        .iter()
        .any(|(kind, value)| *kind == 0 && (value == "*" || value == "*.*"))
}

/// Whether `name` passes the active filter. No filters (or an empty rule set) →
/// match all. Otherwise the name must satisfy any one rule: glob (kind 0) by
/// pattern, or MIME (kind 1) against the type guessed from the name.
fn matches_filter(name: &str, filters: &[Filter], active_filter: usize) -> bool {
    if filters.is_empty() {
        return true;
    }
    let Some((_, rules)) = filters.get(active_filter) else {
        return true;
    };
    if rules.is_empty() {
        return true;
    }
    rules.iter().any(|(kind, value)| match kind {
        1 => matches_mime(name, value),
        // Treat any non-MIME kind as a glob (kind 0 is the only other spec value).
        _ => matches_glob(name, value),
    })
}

/// Whether `name` matches a glob rule. Supports `*`/`*.*` (all), a leading-`*`
/// suffix match (`*.png`), else an exact name.
fn matches_glob(name: &str, glob: &str) -> bool {
    if glob == "*" || glob == "*.*" {
        true
    } else if let Some(suffix) = glob.strip_prefix('*') {
        name.ends_with(suffix)
    } else {
        name == glob
    }
}

/// Whether `name`'s guessed content type matches a MIME rule. Handles a
/// `type/*` wildcard as a prefix match; otherwise exact or subtype-of via gio.
fn matches_mime(name: &str, mime: &str) -> bool {
    let (guessed, _) = gio::content_type_guess(Some(Path::new(name)), None::<&[u8]>);
    if let Some(prefix) = mime.strip_suffix('*') {
        guessed.starts_with(prefix)
    } else {
        guessed == mime || gio::content_type_is_a(&guessed, mime)
    }
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

/// Builds one row of the file-type filter popup.
fn filter_row(label: &str) -> gtk::Label {
    gtk::Label::builder()
        .label(label)
        .xalign(0.0)
        .ellipsize(gtk::pango::EllipsizeMode::End)
        .max_width_chars(40)
        .build()
}

/// Where the chooser's sticky view preferences (sort column/direction, view
/// mode) live: `$XDG_STATE_HOME/wayle/file-chooser-view`.
fn ui_state_path() -> Option<PathBuf> {
    wayle_core::paths::ConfigPaths::state_dir()
        .ok()
        .map(|dir| dir.join("file-chooser-view"))
}

/// Loads the persisted sort/view preferences (`<sort> <asc|desc> <list|grid>`),
/// defaulting any missing/unknown token.
fn load_ui_state() -> (SortColumn, bool, ViewMode) {
    let text = ui_state_path()
        .and_then(|path| std::fs::read_to_string(path).ok())
        .unwrap_or_default();
    let mut tokens = text.split_whitespace();
    let sort = match tokens.next() {
        Some("size") => SortColumn::Size,
        Some("modified") => SortColumn::Modified,
        Some("kind") => SortColumn::Kind,
        _ => SortColumn::Name,
    };
    let asc = tokens.next() != Some("desc");
    let view = if tokens.next() == Some("grid") {
        ViewMode::Grid
    } else {
        ViewMode::List
    };
    (sort, asc, view)
}

/// Persists the sort/view preferences. Best-effort — a failed write only means
/// the preference doesn't stick across restarts.
fn save_ui_state(sort: SortColumn, asc: bool, view: ViewMode) {
    let Some(path) = ui_state_path() else {
        return;
    };
    let sort = match sort {
        SortColumn::Name => "name",
        SortColumn::Size => "size",
        SortColumn::Modified => "modified",
        SortColumn::Kind => "kind",
    };
    let asc = if asc { "asc" } else { "desc" };
    let view = match view {
        ViewMode::List => "list",
        ViewMode::Grid => "grid",
    };
    let _ = std::fs::write(path, format!("{sort} {asc} {view}\n"));
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
fn file_row(
    name: &str,
    entry: &Entry,
    size_w: i32,
    mod_w: i32,
    kind_w: i32,
    is_path: bool,
) -> gtk::Box {
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
            // Search rows show a relative path — ellipsize the MIDDLE so both the
            // leading dirs and the filename stay visible. Plain listings show a
            // basename, where keeping the start (End ellipsize) reads better.
            .ellipsize(if is_path {
                gtk::pango::EllipsizeMode::Middle
            } else {
                gtk::pango::EllipsizeMode::End
            })
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
        .width_request(size_w)
        .build();
    size.add_css_class("file-chooser-cell");
    row.append(&size);

    let modified = gtk::Label::builder()
        .label(format_time(entry.modified))
        .xalign(1.0)
        .width_request(mod_w)
        .build();
    modified.add_css_class("file-chooser-cell");
    row.append(&modified);

    let kind = gtk::Label::builder()
        .label(entry_kind(entry))
        .xalign(0.0)
        .width_request(kind_w)
        .ellipsize(gtk::pango::EllipsizeMode::End)
        .build();
    kind.add_css_class("file-chooser-cell");
    row.append(&kind);

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
            // Break anywhere, not just at word boundaries — dot/underscore
            // filenames have none, and an unbreakable line blows the cell (and
            // the whole sheet) wide.
            .wrap_mode(gtk::pango::WrapMode::WordChar)
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
    widgets
        .sort_kind_btn
        .set_label(&format!("Kind{}", arrow(key == SortColumn::Kind)));
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
        if is_raster_image(path) {
            return image_thumbnail_widget(path, size);
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

/// A fixed `size`×`size` thumbnail for an image file. Returns *immediately* with
/// a blank square placeholder (so opening a folder of images paints instantly),
/// then decodes the thumbnail off the UI thread — bounded by [`thumb_semaphore`]
/// — and swaps in the texture when ready. The square `size_request` + `Cover`
/// fit + clipped overflow normalise every thumbnail to the same box regardless
/// of the source aspect ratio, and reserve the slot so nothing reflows on load.
fn image_thumbnail_widget(path: &Path, size: i32) -> gtk::Widget {
    let picture = gtk::Picture::new();
    picture.set_size_request(size, size);
    picture.set_content_fit(gtk::ContentFit::Cover);
    picture.set_halign(gtk::Align::Center);
    picture.set_valign(gtk::Align::Center);
    picture.set_overflow(gtk::Overflow::Hidden);
    picture.add_css_class("file-chooser-thumb");

    let path = path.to_path_buf();
    let weak = picture.downgrade();
    gtk::glib::spawn_future_local(async move {
        // Hold a permit across the decode so at most N run at once. A raw thread
        // (not tokio) keeps this independent of which executor polls the future.
        let Ok(_permit) = thumb_semaphore().acquire().await else {
            return;
        };
        let (tx, rx) = tokio::sync::oneshot::channel();
        std::thread::spawn(move || {
            let _ = tx.send(decode_thumbnail(&path, size));
        });
        let Ok(Some(decoded)) = rx.await else {
            return;
        };
        if let Some(picture) = weak.upgrade() {
            picture.set_paintable(Some(&build_texture(decoded)));
        }
    });

    picture.upcast()
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

/// A decoded thumbnail ready to upload: raw RGBA8 pixels + dimensions + stride.
/// `Send` (unlike a `gdk::Texture`), so it can cross from a decode thread back
/// to the GTK main thread.
type DecodedThumb = (Vec<u8>, i32, i32, usize);

/// Decodes + downscales an image to RGBA8 off the UI thread. Heavy part of
/// thumbnailing (file read + decode + resize); returns `Send` data so the cheap
/// GPU upload ([`build_texture`]) can happen back on the main thread. Files above
/// a size cap are skipped to keep listing snappy.
fn decode_thumbnail(path: &Path, size: i32) -> Option<DecodedThumb> {
    const MAX_BYTES: u64 = 32 * 1024 * 1024;
    if std::fs::metadata(path).ok()?.len() > MAX_BYTES {
        return None;
    }
    // Resize-to-fill produces an exact square (centre-cropped) at the display
    // size: every thumbnail is then identical dimensions, so rows align and
    // labels start at a fixed offset regardless of the source aspect ratio. The
    // texture matching the display size also avoids `gtk::Picture` rendering at
    // its (larger) natural size and inflating row height.
    let target = size.max(1) as u32;
    let rgba = image::open(path)
        .ok()?
        .resize_to_fill(target, target, image::imageops::FilterType::Triangle)
        .to_rgba8();
    let (width, height) = (rgba.width() as i32, rgba.height() as i32);
    let stride = (rgba.width() * 4) as usize;
    Some((rgba.into_raw(), width, height, stride))
}

/// Builds a CPU-side `gdk::MemoryTexture` from decoded RGBA (same path the
/// wallpaper uses, so it composites under software rendering too). Cheap — call
/// on the main thread.
fn build_texture((pixels, width, height, stride): DecodedThumb) -> gtk::gdk::Texture {
    let bytes = gtk::glib::Bytes::from_owned(pixels);
    gtk::gdk::MemoryTexture::new(
        width,
        height,
        gtk::gdk::MemoryFormat::R8g8b8a8,
        &bytes,
        stride,
    )
    .upcast()
}

/// Bounds concurrent thumbnail decodes so opening a folder full of images can't
/// saturate every core / spawn a thread per file. Sized small — decoding is
/// CPU-bound and we only need to keep a screenful flowing.
fn thumb_semaphore() -> &'static tokio::sync::Semaphore {
    static SEM: std::sync::LazyLock<tokio::sync::Semaphore> =
        std::sync::LazyLock::new(|| tokio::sync::Semaphore::new(4));
    &SEM
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

    // Only the named segments matter for crumbs.
    let names: Vec<_> = tail
        .components()
        .filter_map(|c| match c {
            PathComponent::Normal(n) => Some(n.to_os_string()),
            _ => None,
        })
        .collect();

    append_crumb(bar, &base_label, &acc, input, names.is_empty());

    // Keep the trail short: past this many segments, collapse the middle into a
    // single `…` crumb (which jumps to that hidden ancestor) so the bar never
    // grows wide enough to need scrolling — Home › … › parent › current.
    const MAX_TAIL: usize = 3;
    let collapse_at = names.len().saturating_sub(MAX_TAIL);
    for (i, name) in names.iter().enumerate() {
        acc.push(name);
        // The first time we cross into the kept tail, drop in the `…` crumb that
        // targets everything collapsed before it.
        if collapse_at > 0 && i + 1 == collapse_at {
            bar.append(&crumb_separator());
            append_crumb(bar, "…", &acc, input, false);
            continue;
        }
        if i < collapse_at {
            continue; // hidden behind the ellipsis
        }
        bar.append(&crumb_separator());
        append_crumb(
            bar,
            &name.to_string_lossy(),
            &acc,
            input,
            i + 1 == names.len(),
        );
    }
}

/// Appends one breadcrumb button targeting `target`. The label ellipsizes at a
/// bounded width so a single long directory name can't blow out the bar.
fn append_crumb(
    bar: &gtk::Box,
    label: &str,
    target: &Path,
    input: &Sender<FileChooserInput>,
    is_current: bool,
) {
    let lbl = gtk::Label::builder()
        .label(label)
        .ellipsize(gtk::pango::EllipsizeMode::End)
        .max_width_chars(16)
        .build();
    let button = gtk::Button::builder().child(&lbl).build();
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

/// Wires both column dividers to drag-resize their columns + a resize cursor.
/// Wires the sidebar / list / search / filter widget signals to component
/// messages. Split out of `init` to keep it under the line cap.
fn wire_widget_signals(widgets: &FileChooserWidgets, input: &Sender<FileChooserInput>) {
    widgets.places_list.connect_row_activated({
        let input = input.clone();
        move |_, row| input.emit(FileChooserInput::Place(row.index().max(0) as u32))
    });
    widgets.locations_list.connect_row_activated({
        let input = input.clone();
        move |_, row| input.emit(FileChooserInput::Location(row.index().max(0) as u32))
    });
    widgets.search_entry.connect_search_changed({
        let input = input.clone();
        move |entry| input.emit(FileChooserInput::Search(entry.text().to_string()))
    });
    widgets.file_list.connect_row_activated({
        let input = input.clone();
        move |_, row| input.emit(FileChooserInput::Activate(row.index().max(0) as u32))
    });
    widgets.file_grid.connect_child_activated({
        let input = input.clone();
        move |_, child| input.emit(FileChooserInput::Activate(child.index().max(0) as u32))
    });
    widgets.file_list.connect_selected_rows_changed({
        let input = input.clone();
        move |_| input.emit(FileChooserInput::SelectionChanged)
    });
    widgets.file_grid.connect_selected_children_changed({
        let input = input.clone();
        move |_| input.emit(FileChooserInput::SelectionChanged)
    });
    widgets.name_entry.connect_activate({
        let input = input.clone();
        move |_| input.emit(FileChooserInput::Confirm)
    });
    widgets.filter_list.connect_row_activated({
        let input = input.clone();
        move |_, row| input.emit(FileChooserInput::SelectFilter(row.index().max(0) as u32))
    });
}

/// Column dividers drag-resize their column (live), committing on release so
/// the rows re-lay to match. One gesture on the **header** (a stable frame —
/// it doesn't move while a column resizes), hit-testing which grip the press
/// landed on. Attaching the gesture to the grip itself fed the grip's own
/// displacement back into the drag offset, so the divider tracked the cursor
/// at roughly half speed.
fn install_column_resize(
    widgets: &FileChooserWidgets,
    input: &Sender<FileChooserInput>,
    root: &gtk::Window,
) {
    let grips = [
        (widgets.size_handle.clone(), widgets.sort_size_btn.clone()),
        (
            widgets.mod_handle.clone(),
            widgets.sort_modified_btn.clone(),
        ),
        (widgets.kind_handle.clone(), widgets.sort_kind_btn.clone()),
    ];
    for (handle, _) in &grips {
        handle.set_cursor_from_name(Some("col-resize"));
    }

    let header = widgets.col_header.clone();
    let drag = gtk::GestureDrag::new();
    // The grip under the press + its column's starting width, for the drag's
    // duration; None when the press wasn't on a grip.
    let target: std::rc::Rc<std::cell::RefCell<Option<(gtk::Button, i32)>>> =
        std::rc::Rc::new(std::cell::RefCell::new(None));
    drag.connect_drag_begin({
        let target = target.clone();
        let header = header.clone();
        move |gesture, x, _| {
            // Pad the 7px grip so it isn't a pixel-hunt to grab.
            const SLOP: f32 = 5.0;
            let hit = grips.iter().find_map(|(handle, btn)| {
                let bounds = handle.compute_bounds(&header)?;
                (x as f32 >= bounds.x() - SLOP && x as f32 <= bounds.x() + bounds.width() + SLOP)
                    .then(|| btn.clone())
            });
            match hit {
                Some(btn) => {
                    let base = btn.width().max(btn.width_request());
                    *target.borrow_mut() = Some((btn, base));
                }
                None => {
                    gesture.set_state(gtk::EventSequenceState::Denied);
                }
            }
        }
    });
    drag.connect_drag_update({
        let target = target.clone();
        move |_, offset_x, _| {
            if let Some((btn, base)) = target.borrow().as_ref() {
                let new_w = (f64::from(*base) - offset_x).round() as i32;
                btn.set_width_request(new_w.clamp(50, 400));
            }
        }
    });
    drag.connect_drag_end({
        let input = input.clone();
        move |_, _, _| {
            if target.borrow_mut().take().is_some() {
                input.emit(FileChooserInput::ColumnsResized);
            }
        }
    });
    header.add_controller(drag);

    setup_surface_resize(&widgets.overlay, &widgets.surface, root);
    widgets.resize_grip.set_cursor_from_name(Some("se-resize"));
}

/// Corner-drag resize. The gesture is attached to the **overlay**, not the grip:
/// the overlay's top-left is fixed (the window is anchored top-left and grows
/// down-right), so the press point + drag offset is the cursor's position in a
/// stable frame — i.e. exactly the target width/height. Reading it off the grip
/// instead would feed the grip's own movement back in, so the size would lag the
/// cursor by a constant gap. Only starts when the press lands in the bottom-right
/// corner zone (over the visible grip).
fn setup_surface_resize(overlay: &gtk::Overlay, surface: &gtk::Box, root: &gtk::Window) {
    const CORNER_ZONE: i32 = 28;
    let drag = gtk::GestureDrag::new();
    // The sheet's content floor, captured at drag start: the size below which
    // the sidebar + fixed columns can't compress. Clamping to a measured floor
    // (not a guessed constant) means the grip stops exactly where the layout
    // stops — a constant below the floor left a dead drag range where
    // shrinking silently did nothing, which read as "resize is broken".
    let floor = std::rc::Rc::new(std::cell::Cell::new((400i32, 300i32)));
    drag.connect_drag_begin({
        let overlay = overlay.clone();
        let surface = surface.clone();
        let floor = floor.clone();
        move |gesture, x, y| {
            let in_corner = (x.round() as i32) >= overlay.width() - CORNER_ZONE
                && (y.round() as i32) >= overlay.height() - CORNER_ZONE;
            if !in_corner {
                gesture.set_state(gtk::EventSequenceState::Denied);
                return;
            }
            // Measure the surface's children, not the surface: the surface's
            // own min is the width_request a previous drag set, which would
            // ratchet the floor up to wherever the sheet currently is.
            let (mut floor_w, mut floor_h) = (0i32, 0i32);
            let mut child = surface.first_child();
            while let Some(c) = child {
                if c.is_visible() {
                    let (min_w, _, _, _) = c.measure(gtk::Orientation::Horizontal, -1);
                    let (min_h, _, _, _) = c.measure(gtk::Orientation::Vertical, -1);
                    floor_w = floor_w.max(min_w);
                    floor_h += min_h;
                }
                child = c.next_sibling();
            }
            floor.set((floor_w.max(400), floor_h.max(300)));
        }
    });
    drag.connect_drag_update({
        let surface = surface.clone();
        let root = root.clone();
        let floor = floor.clone();
        // start_point + offset = the cursor's position relative to the overlay's
        // (fixed) top-left, which is the size we want the sheet to be. No feedback
        // from the surface growing, so it tracks the cursor 1:1.
        move |gesture, ox, oy| {
            let Some((sx, sy)) = gesture.start_point() else {
                return;
            };
            let (floor_w, floor_h) = floor.get();
            let w = ((sx + ox).round() as i32).clamp(floor_w, 1600);
            let h = ((sy + oy).round() as i32).clamp(floor_h, 1100);
            surface.set_width_request(w);
            surface.set_height_request(h);
            // The size request alone can fail to shrink the window: GTK keeps
            // the largest configured size once mapped. Re-setting the default
            // size is GTK4's programmatic resize, and guarantees the window
            // follows the drag in both directions.
            root.set_default_size(w, h);
        }
    });
    overlay.add_controller(drag);
}

/// Header-drag to move the sheet (like a titlebar). The header moves with the
/// window, so we add the gesture offset to the window's *live* margin each event
/// rather than to a fixed start margin: after each move the compositor re-reports
/// the pointer relative to the moved surface (offset collapses toward 0), so
/// `live_margin + offset` settles instead of oscillating — that oscillation was
/// the jank.
fn setup_surface_move(header: &gtk::CenterBox, root: &gtk::Window) {
    // Grab/grabbing cursor so the header reads as a draggable titlebar (child
    // buttons keep their own "pointer" cursor). GTK ignores CSS `cursor`.
    header.set_cursor_from_name(Some("grab"));
    let drag = gtk::GestureDrag::new();
    drag.connect_drag_begin({
        let header = header.clone();
        move |_, _, _| header.set_cursor_from_name(Some("grabbing"))
    });
    drag.connect_drag_end({
        let header = header.clone();
        move |_, _, _| header.set_cursor_from_name(Some("grab"))
    });
    drag.connect_drag_update({
        let root = root.clone();
        move |_, dx, dy| {
            let x = (root.margin(Edge::Left) + dx.round() as i32).max(0);
            let y = (root.margin(Edge::Top) + dy.round() as i32).max(0);
            root.set_margin(Edge::Left, x);
            root.set_margin(Edge::Top, y);
        }
    });
    header.add_controller(drag);
}

/// Layer-shell margins (left, top) that centre a `w`×`h` sheet on the primary
/// monitor; falls back to a fixed offset if no monitor geometry is available.
fn center_margins(w: i32, h: i32) -> (i32, i32) {
    let geo = gtk::gdk::Display::default()
        .and_then(|d| d.monitors().item(0))
        .and_downcast::<gtk::gdk::Monitor>()
        .map(|m| m.geometry());
    match geo {
        Some(g) => (((g.width() - w) / 2).max(0), ((g.height() - h) / 2).max(0)),
        None => (200, 150),
    }
}

/// Installs the window's keyboard shortcuts (Escape cancels, spacebar Quick
/// Look) and the drag-and-drop target (drop a path to navigate).
fn install_controllers(
    root: &gtk::Window,
    revealer: &WayleRevealer,
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
        // MIME filter: the type guessed from the name must match the rule.
        let mime = vec![("Img".to_owned(), vec![(1u32, "image/png".to_owned())])];
        assert!(matches_filter("photo.png", &mime, 0));
        assert!(!matches_filter("notes.txt", &mime, 0));
        // MIME wildcard matches any subtype of the family.
        let imgs = vec![("Images".to_owned(), vec![(1u32, "image/*".to_owned())])];
        assert!(matches_filter("a.png", &imgs, 0));
        assert!(matches_filter("a.jpg", &imgs, 0));
        assert!(!matches_filter("a.txt", &imgs, 0));
        // Empty rule set → match all so nothing is hidden.
        let empty = vec![("Any".to_owned(), Vec::new())];
        assert!(matches_filter("x.bin", &empty, 0));
        // Out-of-range active filter → match all.
        assert!(matches_filter("x", &filters, 99));
    }

    #[test]
    fn mime_ext_covers_common_types() {
        // A spread of types apps actually put in file-chooser filters.
        for (mime, want) in [
            ("image/png", "png"),
            ("image/jpeg", "jpg"),
            ("image/svg+xml", "svg"),
            ("image/gif", "gif"),
            ("image/webp", "webp"),
            ("video/mp4", "mp4"),
            ("video/x-matroska", "mkv"),
            ("application/pdf", "pdf"),
            ("application/zip", "zip"),
            ("application/json", "json"),
            ("text/plain", "txt"),
            ("text/csv", "csv"),
            ("text/html", "html"),
            (
                "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
                "docx",
            ),
            // Overrides for mime2ext's missing / obscure media picks.
            ("audio/mpeg", "mp3"),
            ("audio/flac", "flac"),
            ("audio/aac", "aac"),
            ("video/quicktime", "mov"),
        ] {
            assert_eq!(mime_ext(mime), Some(want), "{mime}");
        }
        // Wildcards / unknown types resolve to nothing (caller shows raw MIME).
        assert_eq!(mime_ext("image/*"), None);
        assert_eq!(mime_ext("application/x-made-up"), None);
    }

    #[test]
    fn filter_label_prefers_name_falls_back_to_patterns() {
        // Named filters show the name alone — never the raw rules (apps send
        // walls of case-insensitive globs like `*.[Pp][Nn][Gg]`).
        let g = (
            "Images".to_owned(),
            vec![(0u32, "*.png".to_owned()), (0, "*.[Jj][Pp][Gg]".to_owned())],
        );
        assert_eq!(filter_label(&g), "Images");
        // A nameless filter falls back to its patterns; MIME rules are
        // reverse-mapped to `*.ext` globs, duplicates collapse.
        let unnamed = (
            String::new(),
            vec![(0u32, "*.png".to_owned()), (1, "image/png".to_owned())],
        );
        assert_eq!(filter_label(&unnamed), "*.png");
        let unnamed_mime = (" ".to_owned(), vec![(1u32, "image/jpeg".to_owned())]);
        assert_eq!(filter_label(&unnamed_mime), "*.jpg");
        // Wildcard MIME kept raw — it can't reverse to an extension.
        let w = (String::new(), vec![(1u32, "image/*".to_owned())]);
        assert_eq!(filter_label(&w), "image/*");
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
    fn entry_kind_labels_folder_extension_or_file() {
        assert_eq!(entry_kind(&entry("/x/docs", true, 0)), "Folder");
        assert_eq!(entry_kind(&entry("/x/a.png", false, 0)), "PNG");
        assert_eq!(entry_kind(&entry("/x/archive.TAR", false, 0)), "TAR");
        assert_eq!(entry_kind(&entry("/x/Makefile", false, 0)), "File");
    }

    #[test]
    fn search_ranks_close_and_well_matched_first() {
        let root = Path::new("/home/u");
        let mk = |rel: &str| entry(&format!("/home/u/{rel}"), false, 0);
        let mut entries = vec![
            mk("a/b/c/d/deep-php-helper.rs"), // deep, substring
            mk("php.ini"),                    // shallow, exact basename
            mk("src/php-config.txt"),         // mid, prefix
            mk("vendor/x/y/php"),             // deeper, exact basename
        ];
        sort_search_entries(&mut entries, root, "php");
        let order: Vec<String> = entries.iter().map(|e| entry_name(&e.path)).collect();
        // Match quality is primary (exact > prefix > substring); depth breaks
        // ties so within a bucket shallower paths win. So: exact "php" first,
        // then the two prefix matches shallow-first, then the deep substring.
        assert_eq!(
            order,
            ["php", "php.ini", "php-config.txt", "deep-php-helper.rs"]
        );
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
        // Drives the real off-thread decode (`decode_thumbnail`) on a PNG, so the
        // thumbnail data path is verified without a display/GPU.
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

        let size = 24i32;
        let (pixels, width, height, stride) =
            decode_thumbnail(&path, size).expect("decode thumbnail");
        std::fs::remove_file(&path).ok();

        // Fits within ~2x the display size, RGBA8 (4 bytes/px, stride = w*4), and
        // Exactly a `size`×`size` square (centre-cropped, so every thumbnail is
        // uniform), RGBA8 (4 bytes/px, stride = w*4), and the left edge is reddish
        // (decode preserved real pixels, not blank).
        assert_eq!(width, size);
        assert_eq!(height, size);
        assert_eq!(stride, (width * 4) as usize);
        assert_eq!(pixels.len(), (width * height * 4) as usize);
        assert!(pixels[0] > pixels[2], "left edge should be reddish");
    }

    #[test]
    fn walk_search_recurses_and_honors_hidden() {
        // Build: root/{top_match.txt, plain.txt, sub/nested_match.txt,
        //              .hidden/buried_match.txt, .secret_match.txt}
        let root = std::env::temp_dir().join("wayle_fc_walk_test");
        std::fs::remove_dir_all(&root).ok();
        std::fs::create_dir_all(root.join("sub")).expect("mk sub");
        std::fs::create_dir_all(root.join(".hidden")).expect("mk hidden");
        std::fs::write(root.join("top_match.txt"), b"").unwrap();
        std::fs::write(root.join("plain.txt"), b"").unwrap();
        std::fs::write(root.join("sub/nested_match.txt"), b"").unwrap();
        std::fs::write(root.join(".hidden/buried_match.txt"), b"").unwrap();
        std::fs::write(root.join(".secret_match.txt"), b"").unwrap();

        // Generation 1 is "live" throughout these calls (cancel stays == my_gen).
        let live = AtomicU64::new(1);

        // Hidden excluded: finds the top + nested matches, descends subdirs, but
        // skips the dot-file and never descends the dot-directory.
        let mut got = walk_search(&root, "match", false, &live, 1)
            .iter()
            .map(|p| entry_name(p))
            .collect::<Vec<_>>();
        got.sort();
        assert_eq!(got, ["nested_match.txt", "top_match.txt"]);

        // Case-insensitive query also matches.
        assert_eq!(walk_search(&root, "MATCH", false, &live, 1).len(), 2);

        // With hidden shown: the dot-file and the buried-in-dot-dir match appear.
        let mut got_hidden = walk_search(&root, "match", true, &live, 1)
            .iter()
            .map(|p| entry_name(p))
            .collect::<Vec<_>>();
        got_hidden.sort();
        assert_eq!(
            got_hidden,
            [
                ".secret_match.txt",
                "buried_match.txt",
                "nested_match.txt",
                "top_match.txt"
            ]
        );

        // A superseded generation aborts immediately (no matches collected).
        let superseded = AtomicU64::new(2);
        assert!(walk_search(&root, "match", false, &superseded, 1).is_empty());

        std::fs::remove_dir_all(&root).ok();
    }
}
