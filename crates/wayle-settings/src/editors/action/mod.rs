//! Searchable action editor for `ClickAction` fields.
//!
//! Presents a searchable dropdown of the module's predefined actions (e.g.
//! "Capture region"), its dropdown panel(s), "None", and "Custom command…".
//! Picking "Custom command…" reveals a text entry for a raw shell command. A
//! value that matches no predefined choice loads into the Custom entry.

mod row;

use relm4::{
    gtk,
    gtk::{glib::SignalHandlerId, prelude::*},
    prelude::*,
};
pub(crate) use row::action;
use wayle_config::{ClickAction, ConfigProperty};
use wayle_widgets::prelude::ellipsizing_string_factory;

use super::{WatcherHandle, spawn_property_watcher};

/// A predefined action a module offers in the dropdown.
#[derive(Clone)]
pub(crate) struct ActionChoice {
    /// Display label (already localized / human-readable).
    pub(crate) label: String,
    /// The action stored when this choice is selected.
    pub(crate) action: ClickAction,
}

/// Init for the action editor.
pub(crate) struct ActionInit {
    pub(crate) property: ConfigProperty<ClickAction>,
    pub(crate) choices: Vec<ActionChoice>,
}

pub(crate) struct ActionControl {
    property: ConfigProperty<ClickAction>,
    choices: Vec<ActionChoice>,
    dropdown: gtk::DropDown,
    entry: gtk::Entry,
    revealer: gtk::Revealer,
    dropdown_handler: SignalHandlerId,
    entry_handler: SignalHandlerId,
    /// True once the user explicitly picked "Custom command…", so an empty /
    /// `None` value still shows the entry instead of snapping back to "None".
    custom_mode: bool,
    _watcher: WatcherHandle,
}

#[derive(Debug)]
pub(crate) enum ActionMsg {
    Selected(u32),
    CustomChanged(String),
    Refresh,
}

impl SimpleComponent for ActionControl {
    type Init = ActionInit;
    type Input = ActionMsg;
    type Output = ();
    type Root = gtk::Box;
    type Widgets = ();

    fn init_root() -> Self::Root {
        gtk::Box::builder()
            .orientation(gtk::Orientation::Vertical)
            .spacing(4)
            .hexpand(false)
            .valign(gtk::Align::Center)
            .build()
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let ActionInit { property, choices } = init;

        let labels = labels(&choices);
        let string_list =
            gtk::StringList::new(&labels.iter().map(String::as_str).collect::<Vec<_>>());

        // PropertyExpression over StringObject::string powers the type-ahead search.
        let expression = gtk::PropertyExpression::new(
            gtk::StringObject::static_type(),
            gtk::Expression::NONE,
            "string",
        );
        let dropdown = gtk::DropDown::new(Some(string_list), Some(expression));
        dropdown.set_enable_search(true);
        dropdown.set_factory(Some(&ellipsizing_string_factory()));
        dropdown.set_cursor_from_name(Some("pointer"));

        let entry = gtk::Entry::builder()
            .placeholder_text("Shell command")
            .hexpand(true)
            .build();
        let revealer = gtk::Revealer::builder()
            .transition_type(gtk::RevealerTransitionType::SlideDown)
            .child(&entry)
            .build();

        let current = property.get();
        let custom_mode = matches!(current, ClickAction::Shell(_))
            && index_of_choice(&choices, &current).is_none();
        let index = selection_index(&choices, &current, custom_mode);
        dropdown.set_selected(index);
        if index == custom_index(&choices) {
            entry.set_text(&command_string(&current));
            revealer.set_reveal_child(true);
        }

        let dropdown_handler = {
            let sender = sender.input_sender().clone();
            dropdown.connect_selected_notify(move |dd| {
                let _ = sender.send(ActionMsg::Selected(dd.selected()));
            })
        };
        let entry_handler = {
            let sender = sender.input_sender().clone();
            entry.connect_changed(move |entry| {
                let _ = sender.send(ActionMsg::CustomChanged(entry.text().to_string()));
            })
        };

        let watcher_sender = sender.input_sender().clone();
        let watcher = spawn_property_watcher(&property, move || {
            watcher_sender.send(ActionMsg::Refresh).is_ok()
        });

        root.append(&dropdown);
        root.append(&revealer);

        let model = Self {
            property,
            choices,
            dropdown,
            entry,
            revealer,
            dropdown_handler,
            entry_handler,
            custom_mode,
            _watcher: watcher,
        };

        ComponentParts { model, widgets: () }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>) {
        match msg {
            ActionMsg::Selected(index) => {
                let none_index = self.choices.len() as u32;
                let custom = custom_index(&self.choices);
                if index < none_index {
                    self.custom_mode = false;
                    self.revealer.set_reveal_child(false);
                    self.property
                        .set(self.choices[index as usize].action.clone());
                } else if index == none_index {
                    self.custom_mode = false;
                    self.revealer.set_reveal_child(false);
                    self.property.set(ClickAction::None);
                } else if index == custom {
                    self.custom_mode = true;
                    self.revealer.set_reveal_child(true);
                    self.entry.grab_focus();
                    let text = self.entry.text().to_string();
                    if !text.is_empty() {
                        self.property.set(ClickAction::Shell(text));
                    }
                }
            }
            ActionMsg::CustomChanged(text) => {
                if self.custom_mode {
                    self.property.set(if text.is_empty() {
                        ClickAction::None
                    } else {
                        ClickAction::Shell(text)
                    });
                }
            }
            ActionMsg::Refresh => {
                let current = self.property.get();
                if index_of_choice(&self.choices, &current).is_some()
                    || current == ClickAction::None
                {
                    // A recognized value clears custom mode (unless None was
                    // reached via an in-progress custom edit).
                    if !(self.custom_mode && current == ClickAction::None) {
                        self.custom_mode = false;
                    }
                }
                let index = selection_index(&self.choices, &current, self.custom_mode);

                self.dropdown.block_signal(&self.dropdown_handler);
                self.dropdown.set_selected(index);
                self.dropdown.unblock_signal(&self.dropdown_handler);

                let reveal = index == custom_index(&self.choices);
                self.revealer.set_reveal_child(reveal);
                if reveal {
                    let cmd = command_string(&current);
                    if self.entry.text() != cmd.as_str() {
                        self.entry.block_signal(&self.entry_handler);
                        self.entry.set_text(&cmd);
                        self.entry.unblock_signal(&self.entry_handler);
                    }
                }
            }
        }
    }
}

/// Dropdown labels: choices, then "None", then "Custom command…".
fn labels(choices: &[ActionChoice]) -> Vec<String> {
    let mut labels: Vec<String> = choices.iter().map(|c| c.label.clone()).collect();
    labels.push("None".to_owned());
    labels.push("Custom command…".to_owned());
    labels
}

fn none_index(choices: &[ActionChoice]) -> u32 {
    choices.len() as u32
}

fn custom_index(choices: &[ActionChoice]) -> u32 {
    choices.len() as u32 + 1
}

fn index_of_choice(choices: &[ActionChoice], action: &ClickAction) -> Option<u32> {
    choices
        .iter()
        .position(|c| &c.action == action)
        .map(|i| i as u32)
}

/// Resolves which dropdown row represents `current`.
fn selection_index(choices: &[ActionChoice], current: &ClickAction, custom_mode: bool) -> u32 {
    if let Some(index) = index_of_choice(choices, current) {
        index
    } else if *current == ClickAction::None && !custom_mode {
        none_index(choices)
    } else {
        custom_index(choices)
    }
}

/// The shell-command / dropdown string form of an action (empty for `None`).
fn command_string(action: &ClickAction) -> String {
    match action {
        ClickAction::Shell(cmd) => cmd.clone(),
        ClickAction::Dropdown(name) => format!("dropdown:{name}"),
        ClickAction::None => String::new(),
    }
}
