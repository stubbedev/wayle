mod factory;
mod helpers;
mod messages;
mod methods;
mod watchers;

use std::{rc::Rc, sync::Arc};

use gtk::prelude::*;
use relm4::prelude::*;
use wayle_config::{ClickAction, ConfigProperty, ConfigService, schemas::styling::CssToken};
use wayle_core::DeferredService;
use wayle_power_profiles::PowerProfilesService;
use wayle_widgets::{
    WatcherToken,
    prelude::{BarButton, BarButtonBehavior, BarButtonColors, BarButtonInit, BarButtonOutput},
};

pub(crate) use self::{
    factory::Factory,
    messages::{PowerProfilesCmd, PowerProfilesInit, PowerProfilesMsg},
};
use crate::shell::bar::dropdowns::{self, DropdownRegistry};

pub(crate) struct PowerProfilesModule {
    bar_button: Controller<BarButton>,
    state_watcher: WatcherToken,
    power_profiles: DeferredService<PowerProfilesService>,
    config: Arc<ConfigService>,
    dropdowns: Rc<DropdownRegistry>,
}

#[relm4::component(pub(crate))]
impl Component for PowerProfilesModule {
    type Init = PowerProfilesInit;
    type Input = PowerProfilesMsg;
    type Output = ();
    type CommandOutput = PowerProfilesCmd;

    view! {
        gtk::Box {
            add_css_class: "power-profiles",

            #[local_ref]
            bar_button -> gtk::MenuButton {},
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let config_service = init.config;
        let config = config_service.config().modules.power_profiles.clone();

        let bar_button = BarButton::builder()
            .launch(BarButtonInit {
                icon: config.icon_balanced.get(),
                label: String::new(),
                tooltip: None,
                colors: BarButtonColors {
                    icon_color: config.icon_color.clone(),
                    label_color: config.label_color.clone(),
                    icon_background: config.icon_bg_color.clone(),
                    button_background: config.button_bg_color.clone(),
                    border_color: config.border_color.clone(),
                    auto_icon_color: CssToken::Blue,
                },
                behavior: BarButtonBehavior {
                    label_max_chars: config.label_max_length.clone(),
                    show_icon: config.icon_show.clone(),
                    show_label: config.label_show.clone(),
                    show_border: config.border_show.clone(),
                    visible: ConfigProperty::new(true),
                },
                settings: init.settings,
            })
            .forward(sender.input_sender(), |output| match output {
                BarButtonOutput::LeftClick => PowerProfilesMsg::LeftClick,
                BarButtonOutput::RightClick => PowerProfilesMsg::RightClick,
                BarButtonOutput::MiddleClick => PowerProfilesMsg::MiddleClick,
                BarButtonOutput::ScrollUp => PowerProfilesMsg::ScrollUp,
                BarButtonOutput::ScrollDown => PowerProfilesMsg::ScrollDown,
            });

        watchers::spawn_service_watcher(&sender, &init.power_profiles);
        watchers::spawn_config_watchers(&sender, &config);

        let model = Self {
            bar_button,
            state_watcher: WatcherToken::new(),
            power_profiles: init.power_profiles,
            config: config_service,
            dropdowns: init.dropdowns,
        };
        model.update_display(&config);

        let bar_button = model.bar_button.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>, _root: &Self::Root) {
        let config = &self.config.config().modules.power_profiles;

        let action = match msg {
            PowerProfilesMsg::LeftClick => {
                let action = config.left_click.get();
                if matches!(&action, ClickAction::Shell(s) if s == ":cycle") {
                    self.cycle_profile(&sender);
                    return;
                }
                action
            }
            PowerProfilesMsg::RightClick => config.right_click.get(),
            PowerProfilesMsg::MiddleClick => config.middle_click.get(),
            PowerProfilesMsg::ScrollUp => config.scroll_up.get(),
            PowerProfilesMsg::ScrollDown => config.scroll_down.get(),
        };

        dropdowns::dispatch_click(&action, &self.dropdowns, &self.bar_button);
    }

    fn update_cmd(
        &mut self,
        msg: PowerProfilesCmd,
        sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        let config = &self.config.config().modules.power_profiles;

        match msg {
            PowerProfilesCmd::ServiceReady(service) => {
                watchers::spawn_state_watchers(&sender, self.state_watcher.reset(), &service);
                self.update_display(config);
            }
            PowerProfilesCmd::StateChanged | PowerProfilesCmd::ConfigChanged => {
                self.update_display(config);
            }
        }
    }
}
