//! Editor for the mail `Vec<MailAccount>` field: a card per account with a
//! name, a notmuch query, a provider dropdown (picks the default icon), and an
//! optional icon-name override. Any edit rebuilds the whole `Vec`.

use std::{cell::RefCell, rc::Rc};

use relm4::gtk::{self, prelude::*};
use serde::{
    Deserialize,
    de::{
        IntoDeserializer,
        value::{Error as SerdeValueError, StrDeserializer},
    },
};
use wayle_config::{
    ConfigProperty, EnumVariants,
    schemas::modules::{MailAccount, MailProvider},
};
use wayle_i18n::t;

use crate::{
    editors::{
        icon::{IconPickerWidget, icon_picker_widget},
        list_controls::{add_button, remove_button},
        spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

fn non_empty(text: &str) -> Option<String> {
    (!text.is_empty()).then(|| text.to_owned())
}

/// Deserialize a provider from its serde value (e.g. `"gmail"`).
fn provider_from_value(value: &str) -> MailProvider {
    let de: StrDeserializer<SerdeValueError> = value.into_deserializer();
    MailProvider::deserialize(de).unwrap_or_default()
}

/// The provider for a dropdown index, via the `EnumVariants` order.
fn provider_at(index: u32) -> MailProvider {
    MailProvider::variants()
        .get(index as usize)
        .map_or_else(MailProvider::default, |v| provider_from_value(v.value))
}

/// The dropdown index for a provider.
fn index_of(provider: MailProvider) -> u32 {
    MailProvider::variants()
        .iter()
        .position(|v| provider_from_value(v.value) == provider)
        .unwrap_or(0) as u32
}

fn provider_dropdown(selected: MailProvider) -> gtk::DropDown {
    let labels: Vec<String> = MailProvider::variants()
        .iter()
        .map(|v| t(v.fluent_key))
        .collect();
    let refs: Vec<&str> = labels.iter().map(String::as_str).collect();
    let model = gtk::StringList::new(&refs);
    let dropdown = gtk::DropDown::new(Some(model), gtk::Expression::NONE);
    dropdown.set_selected(index_of(selected));
    dropdown
}

struct Card {
    name: gtk::Entry,
    query: gtk::Entry,
    provider: gtk::DropDown,
    /// Current icon-name override, written by the picker's `set` callback.
    icon: Rc<RefCell<String>>,
    /// Kept alive so the picker's popover + signal closures outlive the card.
    _icon_picker: IconPickerWidget,
}

struct State {
    property: ConfigProperty<Vec<MailAccount>>,
    list: gtk::Box,
    cards: RefCell<Vec<Card>>,
}

impl State {
    fn collected(&self) -> Vec<MailAccount> {
        self.cards
            .borrow()
            .iter()
            .filter(|card| !card.name.text().is_empty())
            .map(|card| MailAccount {
                name: card.name.text().to_string(),
                query: card.query.text().to_string(),
                provider: provider_at(card.provider.selected()),
                icon: non_empty(card.icon.borrow().as_str()),
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
        for account in self.property.get() {
            self.append_card(&account);
        }
    }

    fn append_card(self: &Rc<Self>, account: &MailAccount) {
        let root = gtk::Box::builder()
            .orientation(gtk::Orientation::Vertical)
            .css_classes(["card-form-card"])
            .build();

        let name = entry(&account.name, "name");
        let query = entry(&account.query, "tag:unread and folder:…");
        for e in [&name, &query] {
            let commit_state = Rc::clone(self);
            e.connect_changed(move |_| commit_state.commit());
        }

        let provider = provider_dropdown(account.provider);
        let provider_state = Rc::clone(self);
        provider.connect_selected_notify(move |_| provider_state.commit());

        let icon_value = Rc::new(RefCell::new(account.icon.clone().unwrap_or_default()));
        let set_icon = Rc::clone(&icon_value);
        let icon_state = Rc::clone(self);
        let set: Rc<dyn Fn(&str)> = Rc::new(move |name: &str| {
            *set_icon.borrow_mut() = name.to_string();
            icon_state.commit();
        });
        let icon_picker = icon_picker_widget(&icon_value.borrow(), set);
        icon_picker.widget.set_hexpand(true);

        root.append(&field_row(
            "settings-mail-account-name",
            &name.clone().upcast(),
        ));
        root.append(&field_row(
            "settings-mail-account-query",
            &query.clone().upcast(),
        ));
        root.append(&field_row(
            "settings-mail-account-provider",
            &provider.clone().upcast(),
        ));
        root.append(&field_row(
            "settings-mail-account-icon",
            &icon_picker.widget.clone().upcast(),
        ));

        let remove = remove_button("settings-list-remove");
        remove.set_halign(gtk::Align::End);
        let remove_state = Rc::clone(self);
        let remove_name = name.clone();
        remove.connect_clicked(move |_| remove_state.remove_card(&remove_name));
        root.append(&remove);

        self.list.append(&root);
        self.cards.borrow_mut().push(Card {
            name,
            query,
            provider,
            icon: icon_value,
            _icon_picker: icon_picker,
        });
    }

    fn remove_card(self: &Rc<Self>, name: &gtk::Entry) {
        let index = self
            .cards
            .borrow()
            .iter()
            .position(|card| &card.name == name);
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
        .spacing(8)
        .css_classes(["card-form-row"])
        .build();
    // Fixed-width label on the left so every control starts at the same x; the
    // control fills the rest of the row (entries already hexpand; this also
    // covers the provider DropDown so it spans the row like the entries).
    let label = gtk::Label::builder()
        .label(t(label_key))
        .halign(gtk::Align::Start)
        .css_classes(["card-form-label"])
        .build();
    control.set_hexpand(true);
    row.append(&label);
    row.append(control);
    row
}

/// Full-width row that edits the mail `Vec<MailAccount>` property.
pub(crate) fn mail_account_list(property: &ConfigProperty<Vec<MailAccount>>) -> SettingRowInit {
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
        add_state.append_card(&MailAccount {
            name: String::new(),
            query: String::new(),
            provider: MailProvider::default(),
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
        handle: PropertyHandle::new(property, |value| format!("{} accounts", value.len())),
        control: container.upcast(),
        keepalive: Box::new((state, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}
