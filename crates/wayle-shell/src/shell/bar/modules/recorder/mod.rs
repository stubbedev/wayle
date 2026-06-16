mod factory;
mod helpers;
mod messages;
mod methods;
mod watchers;

use std::{rc::Rc, sync::Arc};

use relm4::{gtk::prelude::*, prelude::*};
use wayle_config::{ConfigProperty, ConfigService, schemas::styling::CssToken};
use wayle_widgets::prelude::{
    BarButton, BarButtonBehavior, BarButtonColors, BarButtonInit, BarButtonOutput,
};

pub(crate) use self::{
    factory::Factory,
    messages::{RecorderCmd, RecorderInit, RecorderMsg},
};
use crate::{
    services::recorder::RecorderState,
    shell::bar::dropdowns::{self, DropdownRegistry},
};

pub(crate) struct RecorderModule {
    bar_button: Controller<BarButton>,
    config: Arc<ConfigService>,
    state: RecorderState,
    dropdowns: Rc<DropdownRegistry>,
}

#[relm4::component(pub(crate))]
impl Component for RecorderModule {
    type Init = RecorderInit;
    type Input = RecorderMsg;
    type Output = ();
    type CommandOutput = RecorderCmd;

    view! {
        gtk::Box {
            add_css_class: "recorder",

            #[local_ref]
            bar_button -> gtk::MenuButton {},
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let config = &init.config.config().modules.recorder;

        let state = init.recorder.state();

        let bar_button = BarButton::builder()
            .launch(BarButtonInit {
                icon: config.icon_idle.get().clone(),
                label: String::new(),
                tooltip: None,
                colors: BarButtonColors {
                    icon_color: config.icon_color.clone(),
                    label_color: config.label_color.clone(),
                    icon_background: config.icon_bg_color.clone(),
                    button_background: config.button_bg_color.clone(),
                    border_color: config.border_color.clone(),
                    auto_icon_color: CssToken::Red,
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
                BarButtonOutput::LeftClick => RecorderMsg::LeftClick,
                BarButtonOutput::RightClick => RecorderMsg::RightClick,
                BarButtonOutput::MiddleClick => RecorderMsg::MiddleClick,
                BarButtonOutput::ScrollUp => RecorderMsg::ScrollUp,
                BarButtonOutput::ScrollDown => RecorderMsg::ScrollDown,
            });

        watchers::spawn_config_watchers(&sender, config);
        watchers::spawn_state_watchers(&sender, &state);

        let model = Self {
            bar_button,
            config: init.config,
            state,
            dropdowns: init.dropdowns,
        };
        model.update_display(&model.config.config().modules.recorder);

        let bar_button = model.bar_button.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, _root: &Self::Root) {
        let config = &self.config.config().modules.recorder;

        let action = match msg {
            RecorderMsg::LeftClick => config.left_click.get(),
            RecorderMsg::RightClick => config.right_click.get(),
            RecorderMsg::MiddleClick => config.middle_click.get(),
            RecorderMsg::ScrollUp => config.scroll_up.get(),
            RecorderMsg::ScrollDown => config.scroll_down.get(),
        };

        dropdowns::dispatch_click(&action, &self.dropdowns, &self.bar_button);
    }

    fn update_cmd(&mut self, msg: RecorderCmd, _sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            RecorderCmd::ConfigChanged | RecorderCmd::StateChanged => {
                let config = &self.config.config().modules.recorder;
                self.update_display(config);
            }
        }
    }
}
