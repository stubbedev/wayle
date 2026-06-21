//! Searchable action editor for click/scroll action fields.
//!
//! Generic over any action type implementing [`ActionValue`] (the bar
//! [`ClickAction`] and the workspace [`WorkspaceClickAction`]). Presents a
//! searchable dropdown of the module's predefined actions, plus "None" and
//! "Custom command…" (which reveals a free-text entry). A value matching no
//! predefined choice loads into the Custom entry.

mod row;

use relm4::{
    gtk,
    gtk::{glib::SignalHandlerId, prelude::*},
    prelude::*,
};
pub(crate) use row::action;
use wayle_config::{
    ClickAction, ConfigProperty, schemas::modules::WorkspaceClickAction,
};
use wayle_widgets::prelude::ellipsizing_string_factory;

use super::{WatcherHandle, spawn_property_watcher};

/// An action value that round-trips through the command string shown in the
/// "Custom" entry (and stored in TOML).
pub(crate) trait ActionValue: Clone + Send + Sync + PartialEq + 'static {
    /// String form: a shell command, `dropdown:<id>`, `focus:<x>`, or empty
    /// for the no-op action.
    fn to_command(&self) -> String;
    /// Parses a command string back into the action (empty = no-op).
    fn from_command(value: &str) -> Self;
}

impl ActionValue for ClickAction {
    fn to_command(&self) -> String {
        match self {
            ClickAction::Shell(cmd) => cmd.clone(),
            ClickAction::Dropdown(name) => format!("dropdown:{name}"),
            ClickAction::Brightness(delta) => format!("brightness:{delta}"),
            ClickAction::BrightnessToggle => "brightness:toggle".to_owned(),
            ClickAction::None => String::new(),
        }
    }

    fn from_command(value: &str) -> Self {
        if value.is_empty() {
            ClickAction::None
        } else if let Some(name) = value.strip_prefix("dropdown:") {
            ClickAction::Dropdown(name.to_owned())
        } else if let Some(rest) = value.strip_prefix("brightness:") {
            match rest {
                "toggle" => ClickAction::BrightnessToggle,
                _ => rest.parse::<i32>().map_or(ClickAction::None, ClickAction::Brightness),
            }
        } else {
            ClickAction::Shell(value.to_owned())
        }
    }
}

impl ActionValue for WorkspaceClickAction {
    fn to_command(&self) -> String {
        match self {
            WorkspaceClickAction::None => String::new(),
            WorkspaceClickAction::FocusWorkspace => "focus:this".to_owned(),
            WorkspaceClickAction::FocusNext => "focus:next".to_owned(),
            WorkspaceClickAction::FocusPrevious => "focus:previous".to_owned(),
            WorkspaceClickAction::FocusLast => "focus:last".to_owned(),
            WorkspaceClickAction::Dropdown(name) => format!("dropdown:{name}"),
            WorkspaceClickAction::Shell(cmd) => cmd.clone(),
        }
    }

    fn from_command(value: &str) -> Self {
        match value {
            "" => WorkspaceClickAction::None,
            "focus:this" => WorkspaceClickAction::FocusWorkspace,
            "focus:next" => WorkspaceClickAction::FocusNext,
            "focus:previous" => WorkspaceClickAction::FocusPrevious,
            "focus:last" => WorkspaceClickAction::FocusLast,
            _ => match value.strip_prefix("dropdown:") {
                Some(name) => WorkspaceClickAction::Dropdown(name.to_owned()),
                None => WorkspaceClickAction::Shell(value.to_owned()),
            },
        }
    }
}

/// A predefined action a module offers in the dropdown.
#[derive(Clone)]
pub(crate) struct ActionChoice<T: ActionValue> {
    /// Display label (already localized / human-readable).
    pub(crate) label: String,
    /// The action stored when this choice is selected.
    pub(crate) action: T,
}

/// Init for the action editor.
pub(crate) struct ActionInit<T: ActionValue> {
    pub(crate) property: ConfigProperty<T>,
    pub(crate) choices: Vec<ActionChoice<T>>,
}

pub(crate) struct ActionControl<T: ActionValue> {
    property: ConfigProperty<T>,
    choices: Vec<ActionChoice<T>>,
    dropdown: gtk::DropDown,
    entry: gtk::Entry,
    revealer: gtk::Revealer,
    dropdown_handler: SignalHandlerId,
    entry_handler: SignalHandlerId,
    /// True once the user explicitly picked "Custom command…", so an empty
    /// value still shows the entry instead of snapping back to "None".
    custom_mode: bool,
    _watcher: WatcherHandle,
}

#[derive(Debug)]
pub(crate) enum ActionMsg {
    Selected(u32),
    CustomChanged(String),
    Refresh,
}

impl<T: ActionValue> SimpleComponent for ActionControl<T> {
    type Init = ActionInit<T>;
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

        let current = property.get().to_command();
        let custom_mode = !current.is_empty() && index_of_choice(&choices, &current).is_none();
        let index = selection_index(&choices, &current, custom_mode);
        dropdown.set_selected(index);
        if index == custom_index(&choices) {
            entry.set_text(&current);
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
                    self.property.set(T::from_command(""));
                } else if index == custom {
                    self.custom_mode = true;
                    self.revealer.set_reveal_child(true);
                    self.entry.grab_focus();
                    let text = self.entry.text().to_string();
                    if !text.is_empty() {
                        self.property.set(T::from_command(&text));
                    }
                }
            }
            ActionMsg::CustomChanged(text) => {
                if self.custom_mode {
                    self.property.set(T::from_command(&text));
                }
            }
            ActionMsg::Refresh => {
                let current = self.property.get().to_command();
                if (index_of_choice(&self.choices, &current).is_some() || current.is_empty())
                    && !(self.custom_mode && current.is_empty()) {
                        self.custom_mode = false;
                    }
                let index = selection_index(&self.choices, &current, self.custom_mode);

                self.dropdown.block_signal(&self.dropdown_handler);
                self.dropdown.set_selected(index);
                self.dropdown.unblock_signal(&self.dropdown_handler);

                let reveal = index == custom_index(&self.choices);
                self.revealer.set_reveal_child(reveal);
                if reveal && self.entry.text() != current.as_str() {
                    self.entry.block_signal(&self.entry_handler);
                    self.entry.set_text(&current);
                    self.entry.unblock_signal(&self.entry_handler);
                }
            }
        }
    }
}

/// Dropdown labels: choices, then "None", then "Custom command…".
fn labels<T: ActionValue>(choices: &[ActionChoice<T>]) -> Vec<String> {
    let mut labels: Vec<String> = choices.iter().map(|c| c.label.clone()).collect();
    labels.push("None".to_owned());
    labels.push("Custom command…".to_owned());
    labels
}

fn none_index<T: ActionValue>(choices: &[ActionChoice<T>]) -> u32 {
    choices.len() as u32
}

fn custom_index<T: ActionValue>(choices: &[ActionChoice<T>]) -> u32 {
    choices.len() as u32 + 1
}

fn index_of_choice<T: ActionValue>(choices: &[ActionChoice<T>], command: &str) -> Option<u32> {
    choices
        .iter()
        .position(|c| c.action.to_command() == command)
        .map(|i| i as u32)
}

/// Resolves which dropdown row represents the current command string.
fn selection_index<T: ActionValue>(
    choices: &[ActionChoice<T>],
    command: &str,
    custom_mode: bool,
) -> u32 {
    if let Some(index) = index_of_choice(choices, command) {
        index
    } else if command.is_empty() && !custom_mode {
        none_index(choices)
    } else {
        custom_index(choices)
    }
}
