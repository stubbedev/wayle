mod factory;
mod helpers;
mod messages;
mod methods;
mod watchers;

use std::{rc::Rc, sync::Arc};

use gtk::prelude::*;
use relm4::prelude::*;
use wayle_config::{ConfigProperty, ConfigService, schemas::styling::CssToken};
use wayle_widgets::prelude::{
    BarButton, BarButtonBehavior, BarButtonColors, BarButtonInit, BarButtonOutput,
};

pub(crate) use self::{
    factory::Factory,
    messages::{MailCmd, MailInit, MailMsg},
};
use crate::shell::bar::dropdowns::{self, DropdownRegistry};

pub(crate) struct MailModule {
    bar_button: Controller<BarButton>,
    config: Arc<ConfigService>,
    dropdowns: Rc<DropdownRegistry>,
    count: u32,
}

#[relm4::component(pub(crate))]
impl Component for MailModule {
    type Init = MailInit;
    type Input = MailMsg;
    type Output = ();
    type CommandOutput = MailCmd;

    view! {
        gtk::Box {
            add_css_class: "mail",

            #[local_ref]
            bar_button -> gtk::MenuButton {},
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let config_service = init.config;
        let config = config_service.config().modules.mail.clone();

        let bar_button = BarButton::builder()
            .launch(BarButtonInit {
                icon: config.icon_name.get(),
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
                BarButtonOutput::LeftClick => MailMsg::LeftClick,
                BarButtonOutput::RightClick => MailMsg::RightClick,
                BarButtonOutput::MiddleClick => MailMsg::MiddleClick,
                BarButtonOutput::ScrollUp => MailMsg::ScrollUp,
                BarButtonOutput::ScrollDown => MailMsg::ScrollDown,
            });

        watchers::spawn_config_watchers(&sender, &config);
        watchers::spawn_total_watcher(&sender, &init.mail);

        // Hidden until the first count arrives (avoids a flash at zero unread).
        if config.hide_when_zero.get() {
            root.set_visible(false);
        }

        let model = Self {
            bar_button,
            config: config_service,
            dropdowns: init.dropdowns,
            count: 0,
        };
        let bar_button = model.bar_button.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, _root: &Self::Root) {
        let config = &self.config.config().modules.mail;

        let action = match msg {
            MailMsg::LeftClick => config.left_click.get(),
            MailMsg::RightClick => config.right_click.get(),
            MailMsg::MiddleClick => config.middle_click.get(),
            MailMsg::ScrollUp => config.scroll_up.get(),
            MailMsg::ScrollDown => config.scroll_down.get(),
        };

        dropdowns::dispatch_click(&action, &self.dropdowns, &self.bar_button);
    }

    fn update_cmd(&mut self, msg: MailCmd, _sender: ComponentSender<Self>, root: &Self::Root) {
        let config = &self.config.config().modules.mail;

        match msg {
            MailCmd::CountChanged(count) => {
                self.count = count;
                self.update_display(config, root);
            }
            MailCmd::ConfigChanged => {
                self.update_display(config, root);
            }
        }
    }
}
