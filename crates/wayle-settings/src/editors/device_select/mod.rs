//! Dropdown control for a string property whose value is a device identifier
//! (e.g. a V4L2 node path), populated from a runtime-enumerated device list.
//!
//! Unlike [`enum_select`](super::enum_select), the option set is discovered at
//! launch rather than derived from the type. The stored value is the device
//! `id`; the dropdown shows human-readable labels. A currently-configured value
//! that is no longer present is preserved as its own entry so opening settings
//! never silently rewrites it.

mod cameras;
mod row;

use relm4::{
    gtk,
    gtk::{glib::SignalHandlerId, prelude::*},
    prelude::*,
};
pub(crate) use row::webcam_device_select;
use wayle_config::ConfigProperty;
use wayle_widgets::prelude::ellipsizing_string_factory;

use super::{WatcherHandle, spawn_property_watcher};

/// A selectable device: `id` is stored in config, `label` is shown.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct DeviceChoice {
    pub id: String,
    pub label: String,
}

pub(crate) struct DeviceSelectInit {
    pub property: ConfigProperty<String>,
    pub choices: Vec<DeviceChoice>,
}

pub(crate) struct DeviceSelectControl {
    property: ConfigProperty<String>,
    choices: Vec<DeviceChoice>,
    dropdown: gtk::DropDown,
    handler_id: SignalHandlerId,
    _watcher: WatcherHandle,
}

#[derive(Debug)]
pub(crate) enum DeviceSelectMsg {
    Selected(u32),
    Refresh,
}

impl SimpleComponent for DeviceSelectControl {
    type Init = DeviceSelectInit;
    type Input = DeviceSelectMsg;
    type Output = ();
    type Root = gtk::Box;
    type Widgets = ();

    fn init_root() -> Self::Root {
        gtk::Box::builder()
            .hexpand(false)
            .valign(gtk::Align::Center)
            .build()
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let mut choices = init.choices;
        let current = init.property.get();
        // Preserve a configured-but-undetected value as its own entry.
        if !current.is_empty() && !choices.iter().any(|c| c.id == current) {
            choices.push(DeviceChoice {
                id: current.clone(),
                label: current.clone(),
            });
        }

        let labels: Vec<&str> = choices.iter().map(|c| c.label.as_str()).collect();
        let string_list = gtk::StringList::new(&labels);
        let current_index = index_of(&choices, &current);

        let dropdown = gtk::DropDown::new(Some(string_list), gtk::Expression::NONE);
        dropdown.set_factory(Some(&ellipsizing_string_factory()));
        dropdown.set_selected(current_index);
        dropdown.set_cursor_from_name(Some("pointer"));

        if let Some(popover) = dropdown
            .last_child()
            .and_then(|child| child.downcast::<gtk::Popover>().ok())
        {
            popover.set_halign(gtk::Align::Center);
        }

        let input_sender = sender.input_sender().clone();
        let handler_id = dropdown.connect_selected_notify(move |dropdown| {
            let _ = input_sender.send(DeviceSelectMsg::Selected(dropdown.selected()));
        });

        let watcher_sender = sender.input_sender().clone();
        let watcher = spawn_property_watcher(&init.property, move || {
            watcher_sender.send(DeviceSelectMsg::Refresh).is_ok()
        });

        root.append(&dropdown);

        let model = Self {
            property: init.property,
            choices,
            dropdown,
            handler_id,
            _watcher: watcher,
        };

        ComponentParts { model, widgets: () }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>) {
        match msg {
            DeviceSelectMsg::Selected(index) => {
                if let Some(choice) = self.choices.get(index as usize) {
                    self.property.set(choice.id.clone());
                }
            }
            DeviceSelectMsg::Refresh => {
                let index = index_of(&self.choices, &self.property.get());
                self.dropdown.block_signal(&self.handler_id);
                self.dropdown.set_selected(index);
                self.dropdown.unblock_signal(&self.handler_id);
            }
        }
    }
}

/// Index of `id` within `choices`, defaulting to 0 (the "default" entry).
fn index_of(choices: &[DeviceChoice], id: &str) -> u32 {
    choices.iter().position(|c| c.id == id).unwrap_or(0) as u32
}
