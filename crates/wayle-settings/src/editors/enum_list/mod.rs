//! Editor for `Vec<enum>` config fields: an ordered list of dropdowns with
//! add/remove/reorder, for enums that derive `EnumVariants` (e.g. the dashboard
//! session actions). Replaces the raw-TOML fallback.
//!
//! Edits read-modify-write the whole `Vec`. Selection changes update in place;
//! structural changes rebuild the rows. A watcher rebuilds on external change.

use std::{cell::RefCell, rc::Rc};

use relm4::gtk::{self, prelude::*};
use serde::{
    Deserialize, Serialize,
    de::value::{Error as SerdeValueError, StrDeserializer},
};
use wayle_config::{ConfigProperty, EnumVariants};
use wayle_i18n::t;

use crate::{
    editors::spawn_property_watcher, pages::spec::SettingRowInit, property_handle::PropertyHandle,
    row::RowBehavior,
};

fn variant_labels<E: EnumVariants>() -> Vec<String> {
    E::variants()
        .iter()
        .map(|variant| {
            let resolved = t(variant.fluent_key);
            if resolved == variant.fluent_key {
                variant.value.to_owned()
            } else {
                resolved
            }
        })
        .collect()
}

fn decode<E: for<'de> Deserialize<'de>>(value: &str) -> Option<E> {
    let de: StrDeserializer<'_, SerdeValueError> = StrDeserializer::new(value);
    E::deserialize(de).ok()
}

fn value_at<E: EnumVariants + for<'de> Deserialize<'de>>(index: u32) -> Option<E> {
    let variant = E::variants().get(index as usize)?;
    decode::<E>(variant.value)
}

fn index_of<E: EnumVariants + PartialEq + for<'de> Deserialize<'de>>(value: &E) -> u32 {
    E::variants()
        .iter()
        .position(|variant| decode::<E>(variant.value).as_ref() == Some(value))
        .unwrap_or(0) as u32
}

struct State<E: EnumVariants + Clone + Send + Sync + PartialEq + 'static> {
    property: ConfigProperty<Vec<E>>,
    list: gtk::Box,
    dropdowns: RefCell<Vec<gtk::DropDown>>,
}

impl<E> State<E>
where
    E: EnumVariants + Clone + Send + Sync + PartialEq + for<'de> Deserialize<'de> + 'static,
{
    fn collected(&self) -> Vec<E> {
        self.dropdowns
            .borrow()
            .iter()
            .filter_map(|dd| value_at::<E>(dd.selected()))
            .collect()
    }

    fn commit(&self) {
        self.property.set(self.collected());
    }

    fn rebuild(self: &Rc<Self>) {
        while let Some(child) = self.list.first_child() {
            self.list.remove(&child);
        }
        self.dropdowns.borrow_mut().clear();
        for value in self.property.get() {
            self.append_row(Some(&value));
        }
    }

    fn append_row(self: &Rc<Self>, value: Option<&E>) {
        let row = gtk::Box::builder()
            .orientation(gtk::Orientation::Horizontal)
            .spacing(4)
            .css_classes(["string-list-row"])
            .build();

        let labels = variant_labels::<E>();
        let model = gtk::StringList::new(&labels.iter().map(String::as_str).collect::<Vec<_>>());
        let dropdown = gtk::DropDown::new(Some(model), gtk::Expression::NONE);
        dropdown.set_hexpand(true);
        if let Some(value) = value {
            dropdown.set_selected(index_of(value));
        }

        let change_state = Rc::clone(self);
        dropdown.connect_selected_notify(move |_| change_state.commit());

        let up = icon_button("ld-chevron-up-symbolic");
        let down = icon_button("ld-chevron-down-symbolic");
        let remove = icon_button("ld-trash-2-symbolic");

        let up_state = Rc::clone(self);
        let up_dd = dropdown.clone();
        up.connect_clicked(move |_| up_state.move_row(&up_dd, -1));
        let down_state = Rc::clone(self);
        let down_dd = dropdown.clone();
        down.connect_clicked(move |_| down_state.move_row(&down_dd, 1));
        let remove_state = Rc::clone(self);
        let remove_dd = dropdown.clone();
        remove.connect_clicked(move |_| remove_state.remove_row(&remove_dd));

        row.append(&dropdown);
        row.append(&up);
        row.append(&down);
        row.append(&remove);

        self.list.append(&row);
        self.dropdowns.borrow_mut().push(dropdown);
    }

    fn move_row(self: &Rc<Self>, dropdown: &gtk::DropDown, delta: i32) {
        let index = self.dropdowns.borrow().iter().position(|dd| dd == dropdown);
        let Some(index) = index else { return };
        let target = index as i32 + delta;
        let mut values = self.collected();
        if target < 0 || target as usize >= values.len() {
            return;
        }
        values.swap(index, target as usize);
        self.property.set(values);
        self.rebuild();
    }

    fn remove_row(self: &Rc<Self>, dropdown: &gtk::DropDown) {
        let index = self.dropdowns.borrow().iter().position(|dd| dd == dropdown);
        if let Some(index) = index {
            let mut values = self.collected();
            values.remove(index);
            self.property.set(values);
            self.rebuild();
        }
    }
}

fn icon_button(icon: &str) -> gtk::Button {
    gtk::Button::builder()
        .icon_name(icon)
        .css_classes(["string-list-button"])
        .valign(gtk::Align::Center)
        .build()
}

/// Full-width row that edits a `Vec<E>` property as a list of dropdowns.
pub(crate) fn enum_list<E>(property: &ConfigProperty<Vec<E>>) -> SettingRowInit
where
    E: EnumVariants
        + Clone
        + Send
        + Sync
        + PartialEq
        + Serialize
        + for<'de> Deserialize<'de>
        + 'static,
{
    let list = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(4)
        .css_classes(["string-list"])
        .build();

    let state = Rc::new(State {
        property: property.clone(),
        list: list.clone(),
        dropdowns: RefCell::new(Vec::new()),
    });
    state.rebuild();

    let add = gtk::Button::builder()
        .label(t("settings-list-add"))
        .css_classes(["string-list-add"])
        .halign(gtk::Align::Start)
        .build();
    let add_state = Rc::clone(&state);
    add.connect_clicked(move |_| {
        add_state.append_row(None);
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
        if watcher_state.property.get() != watcher_state.collected() {
            watcher_state.rebuild();
        }
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value| {
            value
                .iter()
                .filter_map(|v| serde_json::to_string(v).ok())
                .map(|s| s.trim_matches('"').to_owned())
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
