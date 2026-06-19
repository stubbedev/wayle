//! Editor for `Vec<String>` config fields whose entries are icon names: a
//! column of icon pickers with add / remove / reorder controls. Same shell as
//! the [`string_list`](super::string_list) editor, but each row is a
//! preview-driven [`icon_picker_widget`] instead of a bare text entry — used
//! for icon lists like the battery level icons.
//!
//! Edits read-modify-write the whole `Vec` (ConfigProperty is atomic). Each
//! row owns a cell the picker writes; structural changes (add/remove/reorder)
//! rebuild the rows. A watcher rebuilds when the value changes externally
//! (reset, config reload).

use std::{cell::RefCell, rc::Rc};

use relm4::gtk::{self, prelude::*};
use wayle_config::ConfigProperty;

use crate::{
    editors::{
        icon::{IconPickerWidget, icon_picker_widget},
        list_controls::add_button,
        spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

/// One row: the icon value (written by the picker) plus the bits kept alive.
struct IconRow {
    value: Rc<RefCell<String>>,
    /// The picker's trigger, kept for row identity (reorder/remove lookups).
    trigger: gtk::MenuButton,
    /// Kept alive so the picker's popover + signal closures outlive the row.
    _picker: IconPickerWidget,
}

/// Shared mutable state for one icon-list editor instance.
struct IconListState {
    property: ConfigProperty<Vec<String>>,
    list: gtk::Box,
    rows: RefCell<Vec<IconRow>>,
}

impl IconListState {
    /// Pushes the current row values to the property.
    fn commit(&self) {
        self.property.set(self.values());
    }

    /// Current row values (live, includes unsaved edits).
    fn values(&self) -> Vec<String> {
        self.rows
            .borrow()
            .iter()
            .map(|row| row.value.borrow().clone())
            .collect()
    }

    /// Rebuilds the rows from a list of values (used on add/remove/reorder and
    /// on external changes).
    fn rebuild(self: &Rc<Self>, values: &[String]) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.rows.borrow_mut().clear();
        for value in values {
            self.append_row(value);
        }
    }

    /// Builds and appends one icon-picker row, registering its callbacks.
    fn append_row(self: &Rc<Self>, value: &str) {
        let row = gtk::Box::builder()
            .orientation(gtk::Orientation::Horizontal)
            .spacing(4)
            .css_classes(["string-list-row"])
            .build();

        let cell = Rc::new(RefCell::new(value.to_string()));
        let set_cell = Rc::clone(&cell);
        let commit_state = Rc::clone(self);
        let set: Rc<dyn Fn(&str)> = Rc::new(move |name: &str| {
            *set_cell.borrow_mut() = name.to_string();
            commit_state.commit();
        });
        let picker = icon_picker_widget(value, set);
        picker.widget.set_hexpand(true);
        let trigger = picker.widget.clone();

        let up = icon_button("ld-chevron-up-symbolic");
        let down = icon_button("ld-chevron-down-symbolic");
        let remove = icon_button("ld-trash-2-symbolic");

        let move_up_state = Rc::clone(self);
        let up_trigger = trigger.clone();
        up.connect_clicked(move |_| move_up_state.move_row(&up_trigger, -1));

        let move_down_state = Rc::clone(self);
        let down_trigger = trigger.clone();
        down.connect_clicked(move |_| move_down_state.move_row(&down_trigger, 1));

        let remove_state = Rc::clone(self);
        let remove_trigger = trigger.clone();
        remove.connect_clicked(move |_| remove_state.remove_row(&remove_trigger));

        row.append(&picker.widget);
        row.append(&up);
        row.append(&down);
        row.append(&remove);

        self.list.append(&row);
        self.rows.borrow_mut().push(IconRow {
            value: cell,
            trigger,
            _picker: picker,
        });
    }

    fn index_of(&self, trigger: &gtk::MenuButton) -> Option<usize> {
        self.rows
            .borrow()
            .iter()
            .position(|r| &r.trigger == trigger)
    }

    fn remove_row(self: &Rc<Self>, trigger: &gtk::MenuButton) {
        let Some(index) = self.index_of(trigger) else {
            return;
        };
        let mut values = self.values();
        values.remove(index);
        self.rebuild(&values);
        self.commit();
    }

    fn move_row(self: &Rc<Self>, trigger: &gtk::MenuButton, delta: i32) {
        let Some(index) = self.index_of(trigger) else {
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

/// Full-width row that edits a `Vec<String>` of icon names with per-row icon
/// pickers.
pub(crate) fn icon_list(property: &ConfigProperty<Vec<String>>) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(4)
        .css_classes(["string-list"])
        .build();

    let state = Rc::new(IconListState {
        property: property.clone(),
        list: list.clone(),
        rows: RefCell::new(Vec::new()),
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
