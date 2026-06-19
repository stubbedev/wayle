//! Editor for `String -> String` map config fields (icon maps, alias maps,
//! etc.): a column of key/value entry pairs with add/remove. Replaces the
//! raw-TOML fallback.
//!
//! Generic over the concrete map type via [`StringMap`] (covers `BTreeMap` and
//! `HashMap`). Edits read-modify-write the whole map; map equality ignores
//! order, so typing never rebuilds the rows (focus is kept).

use std::{
    cell::RefCell,
    collections::{BTreeMap, HashMap},
    rc::Rc,
};

use relm4::gtk::{self, prelude::*};
use wayle_config::ConfigProperty;
use wayle_i18n::t;

use crate::{
    editors::{list_controls::add_button, spawn_property_watcher},
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

/// A `String -> String` map the editor can read/write generically.
pub(crate) trait StringMap: Clone + Send + Sync + PartialEq + 'static {
    /// Key/value pairs, in a deterministic order where the map has one.
    fn to_pairs(&self) -> Vec<(String, String)>;
    /// Rebuilds the map from pairs (later duplicates win).
    fn from_pairs(pairs: Vec<(String, String)>) -> Self;
}

impl StringMap for BTreeMap<String, String> {
    fn to_pairs(&self) -> Vec<(String, String)> {
        self.iter().map(|(k, v)| (k.clone(), v.clone())).collect()
    }
    fn from_pairs(pairs: Vec<(String, String)>) -> Self {
        pairs.into_iter().collect()
    }
}

impl StringMap for HashMap<String, String> {
    fn to_pairs(&self) -> Vec<(String, String)> {
        let mut pairs: Vec<(String, String)> =
            self.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
        pairs.sort_by(|a, b| a.0.cmp(&b.0));
        pairs
    }
    fn from_pairs(pairs: Vec<(String, String)>) -> Self {
        pairs.into_iter().collect()
    }
}

/// One key/value entry pair.
struct MapRow {
    key: gtk::Entry,
    value: gtk::Entry,
}

/// Shared mutable state for one string-map editor instance.
struct StringMapState<M: StringMap> {
    property: ConfigProperty<M>,
    list: gtk::Box,
    rows: RefCell<Vec<MapRow>>,
}

impl<M: StringMap> StringMapState<M> {
    /// All rows as raw pairs, including in-progress empty ones (kept visible).
    fn current_pairs(&self) -> Vec<(String, String)> {
        self.rows
            .borrow()
            .iter()
            .map(|row| (row.key.text().to_string(), row.value.text().to_string()))
            .collect()
    }

    /// The map to persist: drops rows with an empty key.
    fn committed_map(&self) -> M {
        M::from_pairs(
            self.current_pairs()
                .into_iter()
                .filter(|(key, _)| !key.is_empty())
                .collect(),
        )
    }

    fn commit(&self) {
        self.property.set(self.committed_map());
    }

    fn rebuild(self: &Rc<Self>, pairs: &[(String, String)]) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.rows.borrow_mut().clear();
        for (key, value) in pairs {
            self.append_row(key, value);
        }
    }

    fn append_row(self: &Rc<Self>, key: &str, value: &str) {
        let row = gtk::Box::builder()
            .orientation(gtk::Orientation::Horizontal)
            .spacing(4)
            .css_classes(["string-map-row"])
            .build();

        let key_entry = gtk::Entry::builder()
            .text(key)
            .placeholder_text(t("settings-map-key-placeholder"))
            .hexpand(true)
            .build();
        let value_entry = gtk::Entry::builder()
            .text(value)
            .placeholder_text(t("settings-map-value-placeholder"))
            .hexpand(true)
            .build();

        let key_state = Rc::clone(self);
        key_entry.connect_changed(move |_| key_state.commit());
        let value_state = Rc::clone(self);
        value_entry.connect_changed(move |_| value_state.commit());

        let remove = gtk::Button::builder()
            .icon_name("ld-trash-2-symbolic")
            .css_classes(["string-list-button"])
            .valign(gtk::Align::Center)
            .build();
        remove.set_cursor_from_name(Some("pointer"));
        let remove_state = Rc::clone(self);
        let remove_key = key_entry.clone();
        remove.connect_clicked(move |_| remove_state.remove_row(&remove_key));

        row.append(&key_entry);
        row.append(&value_entry);
        row.append(&remove);

        self.list.append(&row);
        self.rows.borrow_mut().push(MapRow {
            key: key_entry,
            value: value_entry,
        });
    }

    fn remove_row(self: &Rc<Self>, key: &gtk::Entry) {
        let index = self.rows.borrow().iter().position(|row| &row.key == key);
        let Some(index) = index else {
            return;
        };
        self.rows.borrow_mut().remove(index);
        let pairs = self.current_pairs();
        self.rebuild(&pairs);
        self.commit();
    }
}

/// Full-width row that edits a `String -> String` map property.
pub(crate) fn string_map<M: StringMap>(property: &ConfigProperty<M>) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(4)
        .css_classes(["string-map"])
        .build();

    let state = Rc::new(StringMapState {
        property: property.clone(),
        list: list.clone(),
        rows: RefCell::new(Vec::new()),
    });
    state.rebuild(&property.get().to_pairs());

    let add = add_button("settings-map-add");
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        add_state.append_row("", "");
    });

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .hexpand(true)
        .css_classes(["string-list-editor", "string-map-editor"])
        .build();
    container.append(&list);
    container.append(&add);

    let watcher_state = Rc::clone(&state);
    let watcher = spawn_property_watcher(property, move || {
        let value = watcher_state.property.get();
        if value != watcher_state.committed_map() {
            watcher_state.rebuild(&value.to_pairs());
        }
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value| {
            value
                .to_pairs()
                .into_iter()
                .map(|(key, val)| format!("{key} = {val}"))
                .collect::<Vec<_>>()
                .join(", ")
        }),
        control: container.upcast(),
        keepalive: Box::new((state, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
