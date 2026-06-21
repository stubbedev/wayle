//! File chooser host.
//!
//! A headless GTK-thread component that pops the native `gtk::FileDialog` on
//! behalf of the `com.wayle.FileChooser1` D-Bus service (used by the portal's
//! `org.freedesktop.impl.portal.FileChooser`). Runs here because GTK dialogs
//! need the GTK main thread. Replies with the chosen `file://` URIs (empty on
//! cancel).

use relm4::{
    gtk,
    gtk::{gio, prelude::*},
    prelude::*,
};
use tokio::sync::oneshot;

/// A file filter: a display name and `(kind, value)` rules where kind 0 = glob
/// pattern, 1 = MIME type.
pub(crate) type Filter = (String, Vec<(u32, String)>);

/// Messages driving the file chooser host.
pub(crate) enum FileChooserInput {
    /// Open one or more existing files (or a directory).
    Open {
        title: String,
        multiple: bool,
        directory: bool,
        filters: Vec<Filter>,
        current_folder: String,
        reply: oneshot::Sender<Vec<String>>,
    },
    /// Choose a save destination, seeded with `current_name`.
    Save {
        title: String,
        current_name: String,
        filters: Vec<Filter>,
        current_folder: String,
        reply: oneshot::Sender<Vec<String>>,
    },
}

impl std::fmt::Debug for FileChooserInput {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Open {
                title,
                multiple,
                directory,
                ..
            } => f
                .debug_struct("Open")
                .field("title", title)
                .field("multiple", multiple)
                .field("directory", directory)
                .finish_non_exhaustive(),
            Self::Save { title, .. } => f
                .debug_struct("Save")
                .field("title", title)
                .finish_non_exhaustive(),
        }
    }
}

/// The file chooser host component.
pub(crate) struct FileChooser;

#[relm4::component(pub(crate))]
impl Component for FileChooser {
    type Init = ();
    type Input = FileChooserInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        // Headless: owns no visible surface; the dialog is its own window.
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
            model: FileChooser,
            widgets,
        }
    }

    fn update(
        &mut self,
        msg: FileChooserInput,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            FileChooserInput::Open {
                title,
                multiple,
                directory,
                filters,
                current_folder,
                reply,
            } => open(
                &title,
                multiple,
                directory,
                &filters,
                &current_folder,
                reply,
            ),
            FileChooserInput::Save {
                title,
                current_name,
                filters,
                current_folder,
                reply,
            } => save(&title, &current_name, &filters, &current_folder, reply),
        }
    }
}

/// Runs an open/select-folder dialog.
fn open(
    title: &str,
    multiple: bool,
    directory: bool,
    filters: &[Filter],
    current_folder: &str,
    reply: oneshot::Sender<Vec<String>>,
) {
    let dialog = gtk::FileDialog::builder().title(title).modal(true).build();
    apply_filters(&dialog, filters);
    apply_folder(&dialog, current_folder);
    let parent = gtk::Window::NONE;

    if directory {
        dialog.select_folder(parent, gio::Cancellable::NONE, move |result| {
            let _ = reply.send(result.ok().and_then(file_uri).into_iter().collect());
        });
    } else if multiple {
        dialog.open_multiple(parent, gio::Cancellable::NONE, move |result| {
            let _ = reply.send(result.map(list_model_uris).unwrap_or_default());
        });
    } else {
        dialog.open(parent, gio::Cancellable::NONE, move |result| {
            let _ = reply.send(result.ok().and_then(file_uri).into_iter().collect());
        });
    }
}

/// Runs a save dialog.
fn save(
    title: &str,
    current_name: &str,
    filters: &[Filter],
    current_folder: &str,
    reply: oneshot::Sender<Vec<String>>,
) {
    let builder = gtk::FileDialog::builder().title(title).modal(true);
    let dialog = if current_name.is_empty() {
        builder.build()
    } else {
        builder.initial_name(current_name).build()
    };
    apply_filters(&dialog, filters);
    apply_folder(&dialog, current_folder);
    dialog.save(gtk::Window::NONE, gio::Cancellable::NONE, move |result| {
        let _ = reply.send(result.ok().and_then(file_uri).into_iter().collect());
    });
}

/// Builds GTK file filters from the portal spec and sets them on the dialog.
fn apply_filters(dialog: &gtk::FileDialog, filters: &[Filter]) {
    if filters.is_empty() {
        return;
    }
    let store = gio::ListStore::new::<gtk::FileFilter>();
    for (name, rules) in filters {
        let filter = gtk::FileFilter::new();
        filter.set_name(Some(name));
        for (kind, value) in rules {
            if *kind == 1 {
                filter.add_mime_type(value);
            } else {
                filter.add_pattern(value);
            }
        }
        store.append(&filter);
    }
    dialog.set_filters(Some(&store));
}

/// Seeds the dialog's starting directory.
fn apply_folder(dialog: &gtk::FileDialog, current_folder: &str) {
    if !current_folder.is_empty() {
        dialog.set_initial_folder(Some(&gio::File::for_path(current_folder)));
    }
}

/// The `file://` URI of a chosen file, if it has one.
fn file_uri(file: gio::File) -> Option<String> {
    let uri = file.uri();
    (!uri.is_empty()).then(|| uri.to_string())
}

/// Collects the URIs from an `open_multiple` result list model.
fn list_model_uris(model: gio::ListModel) -> Vec<String> {
    let mut uris = Vec::new();
    for index in 0..model.n_items() {
        if let Some(file) = model.item(index).and_downcast::<gio::File>()
            && let Some(uri) = file_uri(file)
        {
            uris.push(uri);
        }
    }
    uris
}
