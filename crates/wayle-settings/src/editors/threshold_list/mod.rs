//! Editor for `Vec<ThresholdEntry>` config fields: a list of cards, each with
//! optional `above`/`below` bounds and five optional color overrides. Replaces
//! the raw-TOML fallback on the metric modules.
//!
//! Edits read-modify-write the whole `Vec`. Each card tracks its index in an
//! `Rc<Cell<usize>>` (reassigned on rebuild) so its inner controls address the
//! right entry. A watcher rebuilds the cards when the entry count changes and
//! otherwise refreshes the inner controls in place.

use std::{
    cell::{Cell, RefCell},
    rc::Rc,
};

use relm4::gtk::{self, prelude::*};
use wayle_config::{
    ConfigProperty,
    schemas::styling::{ColorValue, ThresholdEntry},
};
use wayle_i18n::t;

use crate::{
    editors::{
        card_form::card_titled,
        list_controls::add_button,
        optional::{OptionalWidget, color_value_widget, optional_number_f64_widget},
        spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

const MAX_THRESHOLD: f64 = 100_000.0;
const THRESHOLD_FALLBACK: f64 = 50.0;

const CARD_FIELDS: [&str; 7] = [
    "settings-threshold-above",
    "settings-threshold-below",
    "settings-threshold-icon-color",
    "settings-threshold-label-color",
    "settings-threshold-icon-bg-color",
    "settings-threshold-button-bg-color",
    "settings-threshold-border-color",
];

/// One rendered card: its live position, header title label (retitled from the
/// bounds as they change), and inner controls in `CARD_FIELDS` order.
struct ThresholdCard {
    index: Rc<Cell<usize>>,
    title: gtk::Label,
    controls: Vec<OptionalWidget>,
}

struct ThresholdState {
    property: ConfigProperty<Vec<ThresholdEntry>>,
    list: gtk::Box,
    cards: RefCell<Vec<ThresholdCard>>,
}

/// A card header title derived from the entry's bounds, so it reads as what it
/// matches: a range, a single-sided bound, or a numbered fallback when unbounded.
fn card_title(entry: &ThresholdEntry, number: usize) -> String {
    fn fmt(value: f64) -> String {
        if value.fract() == 0.0 {
            (value as i64).to_string()
        } else {
            value.to_string()
        }
    }
    match (entry.above, entry.below) {
        (Some(above), Some(below)) => format!("{} – {}", fmt(above), fmt(below)),
        (Some(above), None) => format!("≥ {}", fmt(above)),
        (None, Some(below)) => format!("≤ {}", fmt(below)),
        (None, None) => format!("{} {}", t("settings-threshold-card-title"), number),
    }
}

impl ThresholdState {
    fn entries(&self) -> Vec<ThresholdEntry> {
        self.property.get()
    }

    /// Read-modify-write the entry at `index` via `mutate`.
    fn update_entry(self: &Rc<Self>, index: usize, mutate: impl FnOnce(&mut ThresholdEntry)) {
        let mut entries = self.entries();
        if let Some(entry) = entries.get_mut(index) {
            mutate(entry);
            self.property.set(entries);
        }
    }

    fn rebuild(self: &Rc<Self>) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.cards.borrow_mut().clear();
        let count = self.entries().len();
        for index in 0..count {
            self.append_card(index);
        }
    }

    fn append_card(self: &Rc<Self>, index: usize) {
        let card_number = index + 1;
        let index = Rc::new(Cell::new(index));

        let controls = vec![
            self.number_field(&index, |e| e.above, |e, v| e.above = v),
            self.number_field(&index, |e| e.below, |e, v| e.below = v),
            self.color_field(&index, |e| e.icon_color.clone(), |e, v| e.icon_color = v),
            self.color_field(&index, |e| e.label_color.clone(), |e, v| e.label_color = v),
            self.color_field(
                &index,
                |e| e.icon_bg_color.clone(),
                |e, v| e.icon_bg_color = v,
            ),
            self.color_field(
                &index,
                |e| e.button_bg_color.clone(),
                |e, v| {
                    e.button_bg_color = v;
                },
            ),
            self.color_field(
                &index,
                |e| e.border_color.clone(),
                |e, v| e.border_color = v,
            ),
        ];

        let entries = self.entries();
        let title = entries
            .get(index.get())
            .map_or_else(
                || format!("{} {}", t("settings-threshold-card-title"), card_number),
                |entry| card_title(entry, card_number),
            );
        let cw = card_titled(&title);
        for (label_key, control) in CARD_FIELDS.iter().zip(controls.iter()) {
            cw.body.append(&field_row(label_key, &control.widget));
        }

        let remove_state = Rc::clone(self);
        let remove_index = Rc::clone(&index);
        cw.delete.connect_clicked(move |_| {
            let mut entries = remove_state.entries();
            let i = remove_index.get();
            if i < entries.len() {
                entries.remove(i);
                remove_state.property.set(entries);
                remove_state.rebuild();
            }
        });

        self.list.append(&cw.root);

        // `card_titled` always sets a title label; guard rather than unwrap to
        // satisfy the no-panic lint. If it were ever absent the card is simply
        // left untracked instead of crashing the settings window.
        if let Some(title) = cw.title {
            self.cards.borrow_mut().push(ThresholdCard {
                index,
                title,
                controls,
            });
        }
    }

    fn number_field(
        self: &Rc<Self>,
        index: &Rc<Cell<usize>>,
        get: fn(&ThresholdEntry) -> Option<f64>,
        set: fn(&mut ThresholdEntry, Option<f64>),
    ) -> OptionalWidget {
        let get_state = Rc::clone(self);
        let get_index = Rc::clone(index);
        let set_state = Rc::clone(self);
        let set_index = Rc::clone(index);
        optional_number_f64_widget(
            Rc::new(move || get_state.entries().get(get_index.get()).and_then(get)),
            Rc::new(move |value| {
                set_state.update_entry(set_index.get(), |entry| set(entry, value))
            }),
            0.0,
            MAX_THRESHOLD,
            1.0,
            0,
            THRESHOLD_FALLBACK,
        )
    }

    fn color_field(
        self: &Rc<Self>,
        index: &Rc<Cell<usize>>,
        get: fn(&ThresholdEntry) -> Option<ColorValue>,
        set: fn(&mut ThresholdEntry, Option<ColorValue>),
    ) -> OptionalWidget {
        let get_state = Rc::clone(self);
        let get_index = Rc::clone(index);
        let set_state = Rc::clone(self);
        let set_index = Rc::clone(index);
        color_value_widget(
            Rc::new(move || get_state.entries().get(get_index.get()).and_then(get)),
            Rc::new(move |value| {
                set_state.update_entry(set_index.get(), |entry| set(entry, value))
            }),
        )
    }
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

/// Full-width row that edits a `Vec<ThresholdEntry>` property.
pub(crate) fn threshold_list(property: &ConfigProperty<Vec<ThresholdEntry>>) -> SettingRowInit {
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .css_classes(["threshold-list"])
        .build();

    let state = Rc::new(ThresholdState {
        property: property.clone(),
        list: list.clone(),
        cards: RefCell::new(Vec::new()),
    });
    state.rebuild();

    let add = add_button("settings-list-add");
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        let mut entries = add_state.entries();
        entries.push(ThresholdEntry::default());
        add_state.property.set(entries);
        add_state.rebuild();
    });

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(8)
        .hexpand(true)
        .css_classes(["card-form-editor", "threshold-editor"])
        .build();
    container.append(&list);
    container.append(&add);

    let watcher_state = Rc::clone(&state);
    let watcher = spawn_property_watcher(property, move || {
        if watcher_state.entries().len() != watcher_state.cards.borrow().len() {
            watcher_state.rebuild();
        } else {
            let entries = watcher_state.entries();
            for card in watcher_state.cards.borrow().iter() {
                for control in &card.controls {
                    control.refresh();
                }
                if let Some(entry) = entries.get(card.index.get()) {
                    card.title.set_label(&card_title(entry, card.index.get() + 1));
                }
            }
        }
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |entries| {
            format!("{} {}", entries.len(), t("settings-threshold-count"))
        }),
        control: container.upcast(),
        keepalive: Box::new((state, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
