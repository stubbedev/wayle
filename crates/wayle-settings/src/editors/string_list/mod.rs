//! Editor for `Vec<String>` config fields: a column of text entries with
//! add / remove / reorder controls. Replaces the raw-TOML fallback for icon
//! lists, ignore lists, priority lists, etc.
//!
//! Edits read-modify-write the whole `Vec` (ConfigProperty is atomic). Text
//! edits update in place (no rebuild, so focus is kept); structural changes
//! (add/remove/reorder) rebuild the rows. A watcher rebuilds when the value
//! changes externally (reset, config reload).

use std::{cell::RefCell, rc::Rc};

use relm4::gtk::{self, prelude::*};
use wayle_config::ConfigProperty;

use crate::{
    editors::{list_controls::add_button, spawn_property_watcher},
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

/// Shared mutable state for one string-list editor instance.
struct StringListState {
    property: ConfigProperty<Vec<String>>,
    list: gtk::Box,
    entries: RefCell<Vec<gtk::Entry>>,
}

impl StringListState {
    /// Pushes the current entry texts to the property.
    fn commit(&self) {
        let values = self
            .entries
            .borrow()
            .iter()
            .map(|entry| entry.text().to_string())
            .collect::<Vec<_>>();
        self.property.set(values);
    }

    /// Rebuilds the rows from a list of values (used on add/remove/reorder and
    /// on external changes).
    fn rebuild(self: &Rc<Self>, values: &[String]) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.entries.borrow_mut().clear();
        for value in values {
            self.append_row(value);
        }
    }

    /// Builds and appends one entry row, registering its callbacks.
    fn append_row(self: &Rc<Self>, value: &str) {
        let row = gtk::Box::builder()
            .orientation(gtk::Orientation::Horizontal)
            .spacing(4)
            .css_classes(["string-list-row"])
            .build();

        let entry = gtk::Entry::builder().text(value).hexpand(true).build();

        let commit_state = Rc::clone(self);
        entry.connect_changed(move |_| commit_state.commit());

        let up = icon_button("ld-chevron-up-symbolic");
        let down = icon_button("ld-chevron-down-symbolic");
        let remove = icon_button("ld-trash-2-symbolic");

        let move_up_state = Rc::clone(self);
        let up_entry = entry.clone();
        up.connect_clicked(move |_| move_up_state.move_row(&up_entry, -1));

        let move_down_state = Rc::clone(self);
        let down_entry = entry.clone();
        down.connect_clicked(move |_| move_down_state.move_row(&down_entry, 1));

        let remove_state = Rc::clone(self);
        let remove_entry = entry.clone();
        remove.connect_clicked(move |_| remove_state.remove_row(&remove_entry));

        row.append(&entry);
        row.append(&up);
        row.append(&down);
        row.append(&remove);

        self.list.append(&row);
        self.entries.borrow_mut().push(entry);
    }

    fn index_of(&self, entry: &gtk::Entry) -> Option<usize> {
        self.entries.borrow().iter().position(|e| e == entry)
    }

    fn remove_row(self: &Rc<Self>, entry: &gtk::Entry) {
        let Some(index) = self.index_of(entry) else {
            return;
        };
        let mut values = self.values();
        values.remove(index);
        self.rebuild(&values);
        self.commit();
    }

    fn move_row(self: &Rc<Self>, entry: &gtk::Entry, delta: i32) {
        let Some(index) = self.index_of(entry) else {
            return;
        };
        let target = index as i32 + delta;
        let mut values = self.values();
        if target < 0 || target as usize >= values.len() {
            return;
        }
        values.swap(index, target as usize);
        self.rebuild(&values);
        self.commit();
    }

    /// Current entry texts (live, includes unsaved edits).
    fn values(&self) -> Vec<String> {
        self.entries
            .borrow()
            .iter()
            .map(|entry| entry.text().to_string())
            .collect()
    }
}

fn icon_button(icon: &str) -> gtk::Button {
    let button = gtk::Button::builder()
        .icon_name(icon)
        .css_classes(["string-list-button"])
        .valign(gtk::Align::Center)
        .build();
    button.set_cursor_from_name(Some("pointer"));
    button
}

/// Full-width row that edits a `Vec<String>` property.
pub(crate) fn string_list(property: &ConfigProperty<Vec<String>>) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(4)
        .css_classes(["string-list"])
        .build();

    let state = Rc::new(StringListState {
        property: property.clone(),
        list: list.clone(),
        entries: RefCell::new(Vec::new()),
    });
    state.rebuild(&property.get());

    let add = add_button("settings-list-add");
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        add_state.append_row("");
        add_state.commit();
    });

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .hexpand(true)
        .css_classes(["string-list-editor"])
        .build();
    container.append(&list);
    container.append(&add);

    let watcher_state = Rc::clone(&state);
    let watcher = spawn_property_watcher(property, move || {
        let value = watcher_state.property.get();
        if value != watcher_state.values() {
            watcher_state.rebuild(&value);
        }
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value| value.join(", ")),
        control: container.upcast(),
        keepalive: Box::new((state, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
