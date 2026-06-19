//! Editor for the systray `Vec<TrayItemOverride>` field: a row per override
//! with a name pattern, an optional icon, and an optional color. Replaces the
//! raw-TOML fallback.
//!
//! Rows hold their own widget state; any edit rebuilds the whole `Vec` and
//! writes it back.

use std::{cell::RefCell, rc::Rc};

use relm4::gtk::{self, prelude::*};
use wayle_config::{
    ConfigProperty,
    schemas::{modules::TrayItemOverride, styling::ColorValue},
};
use wayle_i18n::t;

use crate::{
    editors::{
        list_controls::add_button, optional::optional_color_widget, spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

fn non_empty(text: &str) -> Option<String> {
    (!text.is_empty()).then(|| text.to_owned())
}

struct Row {
    name: gtk::Entry,
    icon: gtk::Entry,
    color: Rc<RefCell<Option<ColorValue>>>,
}

struct State {
    property: ConfigProperty<Vec<TrayItemOverride>>,
    list: gtk::Box,
    rows: RefCell<Vec<Row>>,
}

impl State {
    fn collected(&self) -> Vec<TrayItemOverride> {
        self.rows
            .borrow()
            .iter()
            .filter(|row| !row.name.text().is_empty())
            .map(|row| TrayItemOverride {
                name: row.name.text().to_string(),
                icon: non_empty(&row.icon.text()),
                color: row.color.borrow().clone(),
            })
            .collect()
    }

    fn commit(&self) {
        self.property.set(self.collected());
    }

    fn rebuild(self: &Rc<Self>) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.rows.borrow_mut().clear();
        for override_entry in self.property.get() {
            self.append_row(&override_entry);
        }
    }

    fn append_row(self: &Rc<Self>, entry: &TrayItemOverride) {
        let root = gtk::Box::builder()
            .orientation(gtk::Orientation::Horizontal)
            .spacing(4)
            .css_classes(["string-map-row"])
            .build();

        let name = text_entry(&entry.name, t("settings-tray-name-placeholder"));
        let icon = text_entry(
            entry.icon.as_deref().unwrap_or(""),
            t("settings-tray-icon-placeholder"),
        );
        for e in [&name, &icon] {
            let commit_state = Rc::clone(self);
            e.connect_changed(move |_| commit_state.commit());
        }

        let color = Rc::new(RefCell::new(entry.color.clone()));
        let color_get = Rc::clone(&color);
        let color_cell = Rc::clone(&color);
        let color_state = Rc::clone(self);
        let color_widget = optional_color_widget(
            Rc::new(move || color_get.borrow().clone()),
            Rc::new(move |value| {
                *color_cell.borrow_mut() = value;
                color_state.commit();
            }),
        );

        let remove = gtk::Button::builder()
            .icon_name("ld-trash-2-symbolic")
            .css_classes(["string-list-button"])
            .valign(gtk::Align::Center)
            .build();
        let remove_state = Rc::clone(self);
        let remove_name = name.clone();
        remove.connect_clicked(move |_| remove_state.remove_row(&remove_name));

        root.append(&name);
        root.append(&icon);
        root.append(&color_widget.widget);
        root.append(&remove);

        self.list.append(&root);
        self.rows.borrow_mut().push(Row { name, icon, color });
    }

    fn remove_row(self: &Rc<Self>, name: &gtk::Entry) {
        let index = self.rows.borrow().iter().position(|row| &row.name == name);
        if let Some(index) = index {
            self.rows.borrow_mut().remove(index);
            self.commit();
            self.rebuild();
        }
    }
}

fn text_entry(text: &str, placeholder: String) -> gtk::Entry {
    gtk::Entry::builder()
        .text(text)
        .placeholder_text(placeholder)
        .hexpand(true)
        .build()
}

/// Full-width row that edits the systray `Vec<TrayItemOverride>` property.
pub(crate) fn tray_override_list(
    property: &ConfigProperty<Vec<TrayItemOverride>>,
) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(4)
        .css_classes(["string-map"])
        .build();

    let state = Rc::new(State {
        property: property.clone(),
        list: list.clone(),
        rows: RefCell::new(Vec::new()),
    });
    state.rebuild();

    let add = add_button("settings-map-add");
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        add_state.append_row(&TrayItemOverride {
            name: String::new(),
            icon: None,
            color: None,
        });
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
        if watcher_state.property.get() != watcher_state.collected() {
            watcher_state.rebuild();
        }
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value| format!("{} overrides", value.len())),
        control: container.upcast(),
        keepalive: Box::new((state, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
