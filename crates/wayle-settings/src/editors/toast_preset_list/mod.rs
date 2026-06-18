//! Editor for the toasts `Vec<ToastPreset>` field: a card per preset with an
//! id, optional label/icon/class text, optional percentage, and optional
//! duration. Replaces the raw-TOML fallback.
//!
//! Rows hold their own widget state (text via entries, optional numbers via
//! cells the widgets write); any edit rebuilds the whole `Vec` and writes it
//! back.

use std::{cell::RefCell, rc::Rc};

use relm4::gtk::{self, prelude::*};
use wayle_config::{ConfigProperty, schemas::toasts::ToastPreset};
use wayle_i18n::t;

use crate::{
    editors::{
        optional::{optional_number_f64_widget, optional_number_widget},
        spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

const MAX_DURATION_MS: u32 = 600_000;
const DURATION_FALLBACK_MS: u32 = 3000;

fn non_empty(text: &str) -> Option<String> {
    (!text.is_empty()).then(|| text.to_owned())
}

struct Card {
    id: gtk::Entry,
    label: gtk::Entry,
    icon: gtk::Entry,
    class: gtk::Entry,
    percentage: Rc<RefCell<Option<f64>>>,
    duration: Rc<RefCell<Option<u32>>>,
}

struct State {
    property: ConfigProperty<Vec<ToastPreset>>,
    list: gtk::Box,
    cards: RefCell<Vec<Card>>,
}

impl State {
    fn collected(&self) -> Vec<ToastPreset> {
        self.cards
            .borrow()
            .iter()
            .filter(|card| !card.id.text().is_empty())
            .map(|card| ToastPreset {
                id: card.id.text().to_string(),
                label: non_empty(&card.label.text()),
                icon: non_empty(&card.icon.text()),
                percentage: *card.percentage.borrow(),
                duration_ms: *card.duration.borrow(),
                class: non_empty(&card.class.text()),
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
        self.cards.borrow_mut().clear();
        for preset in self.property.get() {
            self.append_card(&preset);
        }
    }

    fn append_card(self: &Rc<Self>, preset: &ToastPreset) {
        let root = gtk::Box::builder()
            .orientation(gtk::Orientation::Vertical)
            .css_classes(["threshold-card"])
            .build();

        let id = entry(&preset.id, "id");
        let label = entry(preset.label.as_deref().unwrap_or(""), "label");
        let icon = entry(preset.icon.as_deref().unwrap_or(""), "icon");
        let class = entry(preset.class.as_deref().unwrap_or(""), "css class");
        for e in [&id, &label, &icon, &class] {
            let commit_state = Rc::clone(self);
            e.connect_changed(move |_| commit_state.commit());
        }

        let percentage = Rc::new(RefCell::new(preset.percentage));
        let pct_get = Rc::clone(&percentage);
        let pct_cell = Rc::clone(&percentage);
        let pct_state = Rc::clone(self);
        let pct_widget = optional_number_f64_widget(
            Rc::new(move || *pct_get.borrow()),
            Rc::new(move |value| {
                *pct_cell.borrow_mut() = value;
                pct_state.commit();
            }),
            0.0,
            100.0,
            1.0,
            0,
            50.0,
        );

        let duration = Rc::new(RefCell::new(preset.duration_ms));
        let dur_get = Rc::clone(&duration);
        let dur_cell = Rc::clone(&duration);
        let dur_state = Rc::clone(self);
        let dur_widget = optional_number_widget(
            Rc::new(move || *dur_get.borrow()),
            Rc::new(move |value| {
                *dur_cell.borrow_mut() = value;
                dur_state.commit();
            }),
            0.0,
            f64::from(MAX_DURATION_MS),
            10.0,
            DURATION_FALLBACK_MS,
        );

        root.append(&field_row("settings-toast-preset-id", &id.clone().upcast()));
        root.append(&field_row(
            "settings-toast-preset-label",
            &label.clone().upcast(),
        ));
        root.append(&field_row(
            "settings-toast-preset-icon",
            &icon.clone().upcast(),
        ));
        root.append(&field_row(
            "settings-toast-preset-class",
            &class.clone().upcast(),
        ));
        root.append(&field_row(
            "settings-toast-preset-percentage",
            &pct_widget.widget,
        ));
        root.append(&field_row(
            "settings-toast-preset-duration",
            &dur_widget.widget,
        ));

        let remove = gtk::Button::builder()
            .label(t("settings-list-remove"))
            .css_classes(["string-list-add", "threshold-remove"])
            .halign(gtk::Align::End)
            .build();
        let remove_state = Rc::clone(self);
        let remove_id = id.clone();
        remove.connect_clicked(move |_| remove_state.remove_card(&remove_id));
        root.append(&remove);

        self.list.append(&root);
        self.cards.borrow_mut().push(Card {
            id,
            label,
            icon,
            class,
            percentage,
            duration,
        });
    }

    fn remove_card(self: &Rc<Self>, id: &gtk::Entry) {
        let index = self.cards.borrow().iter().position(|card| &card.id == id);
        if let Some(index) = index {
            self.cards.borrow_mut().remove(index);
            self.commit();
            self.rebuild();
        }
    }
}

fn entry(text: &str, placeholder: &str) -> gtk::Entry {
    gtk::Entry::builder()
        .text(text)
        .placeholder_text(placeholder)
        .hexpand(true)
        .build()
}

fn field_row(label_key: &str, control: &gtk::Widget) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .css_classes(["threshold-row"])
        .build();
    let label = gtk::Label::builder()
        .label(t(label_key))
        .halign(gtk::Align::Start)
        .hexpand(true)
        .css_classes(["threshold-label"])
        .build();
    row.append(&label);
    row.append(control);
    row
}

/// Full-width row that edits the toasts `Vec<ToastPreset>` property.
pub(crate) fn toast_preset_list(property: &ConfigProperty<Vec<ToastPreset>>) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .css_classes(["threshold-list"])
        .build();

    let state = Rc::new(State {
        property: property.clone(),
        list: list.clone(),
        cards: RefCell::new(Vec::new()),
    });
    state.rebuild();

    let add = gtk::Button::builder()
        .label(t("settings-list-add"))
        .css_classes(["string-list-add"])
        .halign(gtk::Align::Start)
        .build();
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        add_state.append_card(&ToastPreset {
            id: String::new(),
            label: None,
            icon: None,
            percentage: None,
            duration_ms: None,
            class: None,
        });
    });

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .hexpand(true)
        .css_classes(["threshold-editor"])
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
        handle: PropertyHandle::new(property, |value| format!("{} presets", value.len())),
        control: container.upcast(),
        keepalive: Box::new((state, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
