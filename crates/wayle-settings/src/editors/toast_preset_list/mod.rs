//! Editor for the toasts `Vec<ToastPreset>` field: a card per preset with a
//! unique id, an optional label, and an optional icon (icon picker). Percentage,
//! duration, and CSS class are runtime-only invoke args, not stored on presets.
//! Replaces the raw-TOML fallback.
//!
//! Cards hold their own widget state; any edit rebuilds the whole `Vec` and
//! writes it back. Duplicate ids are flagged inline as you type.

use std::{cell::RefCell, collections::HashMap, rc::Rc};

use relm4::gtk::{self, prelude::*};
use wayle_config::{ConfigProperty, schemas::osd::ToastPreset};
use wayle_i18n::t;

use crate::{
    editors::{
        card_form::{card, entry, field_row},
        icon::{IconPickerWidget, icon_picker_widget},
        list_controls::add_button,
        spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

fn non_empty(text: &str) -> Option<String> {
    (!text.is_empty()).then(|| text.to_owned())
}

struct Card {
    id: gtk::Entry,
    label: gtk::Entry,
    /// Current icon name, written by the picker's `set` callback.
    icon: Rc<RefCell<String>>,
    /// Kept alive so the picker's popover + signal closures outlive the card.
    _icon_picker: IconPickerWidget,
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
                icon: non_empty(card.icon.borrow().as_str()),
            })
            .collect()
    }

    /// Flags every id entry whose (non-empty) id collides with another card's,
    /// so duplicates are communicated as the user types.
    fn validate(&self) {
        let cards = self.cards.borrow();
        let mut counts: HashMap<String, usize> = HashMap::new();
        for card in cards.iter() {
            let id = card.id.text().to_string();
            if !id.is_empty() {
                *counts.entry(id).or_default() += 1;
            }
        }
        for card in cards.iter() {
            let id = card.id.text().to_string();
            let duplicate = !id.is_empty() && counts.get(&id).copied().unwrap_or(0) > 1;
            if duplicate {
                card.id.add_css_class("error");
                card.id
                    .set_secondary_icon_name(Some("ld-alert-triangle-symbolic"));
                card.id.set_secondary_icon_tooltip_text(Some(&t(
                    "settings-toast-preset-id-duplicate",
                )));
            } else {
                card.id.remove_css_class("error");
                card.id.set_secondary_icon_name(None);
            }
        }
    }

    fn commit(&self) {
        self.property.set(self.collected());
        self.validate();
    }

    fn rebuild(self: &Rc<Self>) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.cards.borrow_mut().clear();
        for preset in self.property.get() {
            self.append_card(&preset);
        }
        self.validate();
    }

    fn append_card(self: &Rc<Self>, preset: &ToastPreset) {
        let id = entry(&preset.id, "id");
        let label = entry(preset.label.as_deref().unwrap_or(""), "label");
        for e in [&id, &label] {
            let commit_state = Rc::clone(self);
            e.connect_changed(move |_| commit_state.commit());
        }

        let icon_value = Rc::new(RefCell::new(preset.icon.clone().unwrap_or_default()));
        let set_icon = Rc::clone(&icon_value);
        let icon_state = Rc::clone(self);
        let set: Rc<dyn Fn(&str)> = Rc::new(move |name: &str| {
            *set_icon.borrow_mut() = name.to_string();
            icon_state.commit();
        });
        let icon_picker = icon_picker_widget(&icon_value.borrow(), set);
        icon_picker.widget.set_hexpand(true);

        let cw = card("settings-toast-preset-id", &id.clone().upcast());
        cw.body.append(&field_row(
            "settings-toast-preset-label",
            &label.clone().upcast(),
        ));
        cw.body.append(&field_row(
            "settings-toast-preset-icon",
            &icon_picker.widget.clone().upcast(),
        ));

        let remove_state = Rc::clone(self);
        let remove_id = id.clone();
        cw.delete
            .connect_clicked(move |_| remove_state.remove_card(&remove_id));

        self.list.append(&cw.root);
        self.cards.borrow_mut().push(Card {
            id,
            label,
            icon: icon_value,
            _icon_picker: icon_picker,
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

/// Full-width row that edits the toasts `Vec<ToastPreset>` property.
pub(crate) fn toast_preset_list(property: &ConfigProperty<Vec<ToastPreset>>) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .css_classes(["card-form-list"])
        .build();

    let state = Rc::new(State {
        property: property.clone(),
        list: list.clone(),
        cards: RefCell::new(Vec::new()),
    });
    state.rebuild();

    let add = add_button("settings-list-add");
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        add_state.append_card(&ToastPreset {
            id: String::new(),
            label: None,
            icon: None,
        });
    });

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .hexpand(true)
        .css_classes(["card-form-editor"])
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
