//! Editor for `key -> WorkspaceStyle` map config fields (workspace/tag style
//! maps): a row per entry with a key plus icon/label text and an optional
//! color. Replaces the raw-TOML fallback.
//!
//! Generic over the concrete map type via [`WorkspaceMap`] (covers integer- and
//! string-keyed `BTreeMap`/`HashMap`). Rows hold their own widget state; any
//! edit rebuilds the whole map and writes it back.

use std::{
    cell::RefCell,
    collections::{BTreeMap, HashMap},
    rc::Rc,
};

use relm4::gtk::{self, prelude::*};
use wayle_config::{
    ConfigProperty,
    schemas::{
        modules::{NiriWorkspaceMap, WorkspaceMap as HyprlandWorkspaceMap, WorkspaceStyle},
        styling::ColorValue,
    },
};
use wayle_i18n::t;

use crate::{
    editors::{
        icon::{IconPickerWidget, icon_picker_widget},
        optional::optional_color_widget,
        spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

/// A `key -> WorkspaceStyle` map the editor can read/write generically. Keys
/// are surfaced as strings; `from_entries` parses them back and drops invalid
/// or empty keys.
pub(crate) trait StyleMap: Clone + Send + Sync + PartialEq + 'static {
    fn to_entries(&self) -> Vec<(String, WorkspaceStyle)>;
    fn from_entries(entries: Vec<(String, WorkspaceStyle)>) -> Self;
}

impl StyleMap for HyprlandWorkspaceMap {
    fn to_entries(&self) -> Vec<(String, WorkspaceStyle)> {
        self.iter()
            .map(|(k, v)| (k.to_string(), v.clone()))
            .collect()
    }
    fn from_entries(entries: Vec<(String, WorkspaceStyle)>) -> Self {
        entries
            .into_iter()
            .filter_map(|(k, v)| k.trim().parse::<i32>().ok().map(|k| (k, v)))
            .collect::<BTreeMap<i32, WorkspaceStyle>>()
            .into()
    }
}

impl StyleMap for NiriWorkspaceMap {
    fn to_entries(&self) -> Vec<(String, WorkspaceStyle)> {
        self.iter().map(|(k, v)| (k.clone(), v.clone())).collect()
    }
    fn from_entries(entries: Vec<(String, WorkspaceStyle)>) -> Self {
        entries
            .into_iter()
            .filter(|(k, _)| !k.is_empty())
            .collect::<BTreeMap<String, WorkspaceStyle>>()
            .into()
    }
}

impl StyleMap for HashMap<String, WorkspaceStyle> {
    fn to_entries(&self) -> Vec<(String, WorkspaceStyle)> {
        let mut entries: Vec<_> = self.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
        entries.sort_by(|a, b| a.0.cmp(&b.0));
        entries
    }
    fn from_entries(entries: Vec<(String, WorkspaceStyle)>) -> Self {
        entries.into_iter().filter(|(k, _)| !k.is_empty()).collect()
    }
}

fn non_empty(text: &str) -> Option<String> {
    (!text.is_empty()).then(|| text.to_owned())
}

struct Row {
    key: gtk::Entry,
    icon: Rc<RefCell<Option<String>>>,
    label: gtk::Entry,
    color: Rc<RefCell<Option<ColorValue>>>,
    /// Kept alive so the icon picker's popover + signal closures outlive the row.
    _icon_picker: IconPickerWidget,
}

struct MapState<M: StyleMap> {
    property: ConfigProperty<M>,
    list: gtk::Box,
    rows: RefCell<Vec<Row>>,
}

impl<M: StyleMap> MapState<M> {
    /// Builds the map from the live rows.
    fn collected(&self) -> M {
        let entries = self
            .rows
            .borrow()
            .iter()
            .map(|row| {
                (
                    row.key.text().to_string(),
                    WorkspaceStyle {
                        icon: row.icon.borrow().clone(),
                        color: row.color.borrow().clone(),
                        label: non_empty(&row.label.text()),
                    },
                )
            })
            .collect();
        M::from_entries(entries)
    }

    fn commit(&self) {
        self.property.set(self.collected());
    }

    fn rebuild(self: &Rc<Self>) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.rows.borrow_mut().clear();
        for (key, style) in self.property.get().to_entries() {
            self.append_row(&key, &style);
        }
    }

    fn append_row(self: &Rc<Self>, key: &str, style: &WorkspaceStyle) {
        let root = gtk::Box::builder()
            .orientation(gtk::Orientation::Horizontal)
            .spacing(4)
            .css_classes(["string-map-row"])
            .build();

        let key_entry = entry(key, t("settings-workspace-key-placeholder"));
        let label_entry = entry(
            style.label.as_deref().unwrap_or(""),
            t("settings-workspace-label-placeholder"),
        );

        for e in [&key_entry, &label_entry] {
            let commit_state = Rc::clone(self);
            e.connect_changed(move |_| commit_state.commit());
        }

        // Icon: a preview-driven picker (same component as the standalone icon
        // fields) instead of a bare text entry. Its value lives in a cell the
        // picker writes; collecting reads the cell.
        let icon_value = Rc::new(RefCell::new(style.icon.clone()));
        let icon_cell = Rc::clone(&icon_value);
        let icon_state = Rc::clone(self);
        let icon_set: Rc<dyn Fn(&str)> = Rc::new(move |name: &str| {
            *icon_cell.borrow_mut() = non_empty(name);
            icon_state.commit();
        });
        let icon_picker = icon_picker_widget(style.icon.as_deref().unwrap_or(""), icon_set);
        icon_picker.widget.set_hexpand(true);

        let color = Rc::new(RefCell::new(style.color.clone()));
        let color_get = Rc::clone(&color);
        let color_set_cell = Rc::clone(&color);
        let color_state = Rc::clone(self);
        let color_widget = optional_color_widget(
            Rc::new(move || color_get.borrow().clone()),
            Rc::new(move |value| {
                *color_set_cell.borrow_mut() = value;
                color_state.commit();
            }),
        );

        let remove = gtk::Button::builder()
            .icon_name("ld-trash-2-symbolic")
            .css_classes(["string-list-button"])
            .valign(gtk::Align::Center)
            .build();
        let remove_state = Rc::clone(self);
        let remove_key = key_entry.clone();
        remove.connect_clicked(move |_| remove_state.remove_row(&remove_key));

        root.append(&key_entry);
        root.append(&icon_picker.widget);
        root.append(&label_entry);
        root.append(&color_widget.widget);
        root.append(&remove);

        self.list.append(&root);
        self.rows.borrow_mut().push(Row {
            key: key_entry,
            icon: icon_value,
            label: label_entry,
            color,
            _icon_picker: icon_picker,
        });
    }

    fn remove_row(self: &Rc<Self>, key: &gtk::Entry) {
        let index = self.rows.borrow().iter().position(|row| &row.key == key);
        if let Some(index) = index {
            self.rows.borrow_mut().remove(index);
            self.commit();
            self.rebuild();
        }
    }
}

fn entry(text: &str, placeholder: String) -> gtk::Entry {
    gtk::Entry::builder()
        .text(text)
        .placeholder_text(placeholder)
        .hexpand(true)
        .build()
}

/// Full-width row that edits a `key -> WorkspaceStyle` map property.
pub(crate) fn workspace_style_map<M: StyleMap>(property: &ConfigProperty<M>) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(4)
        .css_classes(["string-map"])
        .build();

    let state = Rc::new(MapState {
        property: property.clone(),
        list: list.clone(),
        rows: RefCell::new(Vec::new()),
    });
    state.rebuild();

    let add = gtk::Button::builder()
        .label(t("settings-map-add"))
        .css_classes(["string-list-add"])
        .halign(gtk::Align::Start)
        .build();
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        add_state.append_row("", &WorkspaceStyle::default());
    });

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .hexpand(true)
        .css_classes(["string-list-editor", "workspace-style-editor"])
        .build();
    container.append(&list);
    container.append(&add);

    let watcher_state = Rc::clone(&state);
    let watcher = spawn_property_watcher(property, move || {
        if watcher_state.property.get() != watcher_state.collected() {
            watcher_state.rebuild();
        }
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value| {
            format!("{} entries", value.to_entries().len())
        }),
        control: container.upcast(),
        keepalive: Box::new((state, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
