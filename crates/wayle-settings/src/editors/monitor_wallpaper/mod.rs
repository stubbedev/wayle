//! Per-monitor wallpaper editor. Each monitor gets a card with name,
//! wallpaper file picker, and fit mode dropdown. Add/remove monitors.

mod card;
mod methods;
mod row;

use card::{MonitorCard, MonitorCardOutput};
use relm4::{gtk, gtk::prelude::*, prelude::*};
pub(crate) use row::monitor_wallpaper;
use wayle_config::{ConfigProperty, schemas::wallpaper::MonitorWallpaperConfig};

use super::{WatcherHandle, list_controls::add_button, spawn_property_watcher};

pub(crate) struct MonitorWallpaperControl {
    pub(super) property: ConfigProperty<Vec<MonitorWallpaperConfig>>,
    cards: FactoryVecDeque<MonitorCard>,
    _watcher: WatcherHandle,
}

#[derive(Debug)]
pub(crate) enum MonitorWallpaperMsg {
    Add,
    Remove(DynamicIndex),
    CardChanged,
    Refresh,
}

impl SimpleComponent for MonitorWallpaperControl {
    type Init = ConfigProperty<Vec<MonitorWallpaperConfig>>;
    type Input = MonitorWallpaperMsg;
    type Output = ();
    type Root = gtk::Box;
    type Widgets = ();

    fn init_root() -> Self::Root {
        gtk::Box::builder()
            .orientation(gtk::Orientation::Vertical)
            .hexpand(true)
            .build()
    }

    fn init(
        property: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        root.add_css_class("monitor-wallpaper-control");

        let card_list = gtk::Box::builder()
            .orientation(gtk::Orientation::Vertical)
            .build();
        card_list.add_css_class("monitor-wallpaper-list");

        let mut cards = FactoryVecDeque::builder()
            .launch(card_list.clone())
            .forward(sender.input_sender(), |output| match output {
                MonitorCardOutput::Remove(index) => MonitorWallpaperMsg::Remove(index),
                MonitorCardOutput::Changed => MonitorWallpaperMsg::CardChanged,
            });

        {
            let mut guard = cards.guard();
            for config in property.get() {
                guard.push_back(config);
            }
        }

        let add = add_button("settings-monitor-add");

        let input_sender = sender.input_sender().clone();
        add.connect_clicked(move |_button| {
            let _ = input_sender.send(MonitorWallpaperMsg::Add);
        });

        let input_sender = sender.input_sender().clone();
        let watcher = spawn_property_watcher(&property, move || {
            input_sender.send(MonitorWallpaperMsg::Refresh).is_ok()
        });

        root.append(&card_list);
        root.append(&add);

        let model = Self {
            property,
            cards,
            _watcher: watcher,
        };

        ComponentParts { model, widgets: () }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>) {
        match msg {
            MonitorWallpaperMsg::Add => self.on_add(),
            MonitorWallpaperMsg::Remove(index) => self.on_remove(index),
            MonitorWallpaperMsg::CardChanged => self.commit(),
            MonitorWallpaperMsg::Refresh => self.on_refresh(),
        }
    }
}
