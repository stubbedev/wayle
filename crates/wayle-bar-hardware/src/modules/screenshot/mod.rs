mod factory;
mod messages;

use std::{rc::Rc, sync::Arc};

use relm4::{gtk::prelude::*, prelude::*};
use wayle_config::{
    ConfigProperty, ConfigService,
    schemas::{modules::ScreenshotConfig, styling::CssToken},
};
use wayle_widgets::{
    prelude::{
        BarButton, BarButtonBehavior, BarButtonColors, BarButtonInit, BarButtonInput,
        BarButtonOutput,
    },
    watch,
};

pub use self::{
    factory::Factory,
    messages::{ScreenshotCmd, ScreenshotInit, ScreenshotMsg},
};
use crate::shell::bar::dropdowns::{self, DropdownRegistry};

/// Bar button that triggers `wayle screenshot ...` capture commands.
pub struct ScreenshotModule {
    bar_button: Controller<BarButton>,
    config: Arc<ConfigService>,
    dropdowns: Rc<DropdownRegistry>,
}

#[relm4::component(pub)]
impl Component for ScreenshotModule {
    type Init = ScreenshotInit;
    type Input = ScreenshotMsg;
    type Output = ();
    type CommandOutput = ScreenshotCmd;

    view! {
        gtk::Box {
            add_css_class: "screenshot",

            #[local_ref]
            bar_button -> gtk::MenuButton {},
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let config = &init.config.config().modules.screenshot;

        let bar_button = BarButton::builder()
            .launch(BarButtonInit {
                icon: config.icon.get().clone(),
                label: config.label.get().clone(),
                tooltip: None,
                colors: BarButtonColors {
                    icon_color: config.icon_color.clone(),
                    label_color: config.label_color.clone(),
                    icon_background: config.icon_bg_color.clone(),
                    button_background: config.button_bg_color.clone(),
                    border_color: config.border_color.clone(),
                    auto_icon_color: CssToken::Accent,
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
                BarButtonOutput::LeftClick => ScreenshotMsg::LeftClick,
                BarButtonOutput::RightClick => ScreenshotMsg::RightClick,
                BarButtonOutput::MiddleClick => ScreenshotMsg::MiddleClick,
                BarButtonOutput::ScrollUp => ScreenshotMsg::ScrollUp,
                BarButtonOutput::ScrollDown => ScreenshotMsg::ScrollDown,
            });

        spawn_config_watchers(&sender, config);

        let model = Self {
            bar_button,
            config: init.config,
            dropdowns: init.dropdowns,
        };

        let bar_button = model.bar_button.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, _root: &Self::Root) {
        let config = &self.config.config().modules.screenshot;

        let action = match msg {
            ScreenshotMsg::LeftClick => config.left_click.get(),
            ScreenshotMsg::RightClick => config.right_click.get(),
            ScreenshotMsg::MiddleClick => config.middle_click.get(),
            ScreenshotMsg::ScrollUp => config.scroll_up.get(),
            ScreenshotMsg::ScrollDown => config.scroll_down.get(),
        };

        dropdowns::dispatch_click(&action, &self.dropdowns, &self.bar_button);
    }

    fn update_cmd(
        &mut self,
        msg: ScreenshotCmd,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            ScreenshotCmd::ConfigChanged => {
                let config = &self.config.config().modules.screenshot;
                self.bar_button
                    .emit(BarButtonInput::SetIcon(config.icon.get().clone()));
                self.bar_button
                    .emit(BarButtonInput::SetLabel(config.label.get().clone()));
            }
        }
    }
}

fn spawn_config_watchers(sender: &ComponentSender<ScreenshotModule>, config: &ScreenshotConfig) {
    let icon = config.icon.clone();
    let label = config.label.clone();

    watch!(sender, [icon.watch(), label.watch()], |out| {
        let _ = out.send(ScreenshotCmd::ConfigChanged);
    });
}
