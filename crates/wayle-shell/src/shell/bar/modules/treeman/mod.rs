mod factory;
mod helpers;
mod messages;
mod watchers;

use std::{rc::Rc, sync::Arc};

use gtk::prelude::*;
use relm4::prelude::*;
use wayle_config::{ConfigProperty, ConfigService, schemas::styling::CssToken};
use wayle_widgets::{
    prelude::{
        BarButton, BarButtonBehavior, BarButtonColors, BarButtonInit, BarButtonInput,
        BarButtonOutput,
    },
    utils::force_window_resize,
};

pub(crate) use self::{
    factory::Factory,
    messages::{TreemanCmd, TreemanInit, TreemanMsg},
};
use crate::shell::bar::dropdowns::{self, DropdownRegistry};

pub(crate) struct TreemanModule {
    bar_button: Controller<BarButton>,
    config: Arc<ConfigService>,
    dropdowns: Rc<DropdownRegistry>,
    /// Severity class currently applied to the root, so it can be swapped
    /// without leaking stale classes.
    severity: Option<&'static str>,
}

#[relm4::component(pub(crate))]
impl Component for TreemanModule {
    type Init = TreemanInit;
    type Input = TreemanMsg;
    type Output = ();
    type CommandOutput = TreemanCmd;

    view! {
        gtk::Box {
            add_css_class: "treeman",

            #[local_ref]
            bar_button -> gtk::MenuButton {},
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let config = init.config.config();
        let treeman_config = &config.modules.treeman;

        let bar_button = BarButton::builder()
            .launch(BarButtonInit {
                icon: treeman_config.icon_name.get().clone(),
                label: String::from("--"),
                tooltip: None,
                colors: BarButtonColors {
                    icon_color: treeman_config.icon_color.clone(),
                    label_color: treeman_config.label_color.clone(),
                    icon_background: treeman_config.icon_bg_color.clone(),
                    button_background: treeman_config.button_bg_color.clone(),
                    border_color: treeman_config.border_color.clone(),
                    auto_icon_color: CssToken::Accent,
                },
                behavior: BarButtonBehavior {
                    label_max_chars: treeman_config.label_max_length.clone(),
                    show_icon: treeman_config.icon_show.clone(),
                    show_label: treeman_config.label_show.clone(),
                    show_border: treeman_config.border_show.clone(),
                    visible: ConfigProperty::new(true),
                },
                settings: init.settings,
            })
            .forward(sender.input_sender(), |output| match output {
                BarButtonOutput::LeftClick => TreemanMsg::LeftClick,
                BarButtonOutput::RightClick => TreemanMsg::RightClick,
                BarButtonOutput::MiddleClick => TreemanMsg::MiddleClick,
                BarButtonOutput::ScrollUp => TreemanMsg::ScrollUp,
                BarButtonOutput::ScrollDown => TreemanMsg::ScrollDown,
            });

        watchers::spawn_watchers(&sender, treeman_config, &init.treeman);

        let model = Self {
            bar_button,
            config: init.config,
            dropdowns: init.dropdowns,
            severity: None,
        };
        let bar_button = model.bar_button.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, _root: &Self::Root) {
        let treeman = &self.config.config().modules.treeman;

        let action = match msg {
            TreemanMsg::LeftClick => treeman.left_click.get(),
            TreemanMsg::RightClick => treeman.right_click.get(),
            TreemanMsg::MiddleClick => treeman.middle_click.get(),
            TreemanMsg::ScrollUp => treeman.scroll_up.get(),
            TreemanMsg::ScrollDown => treeman.scroll_down.get(),
        };

        dropdowns::dispatch_click(&action, &self.dropdowns, &self.bar_button);
    }

    fn update_cmd(&mut self, msg: TreemanCmd, _sender: ComponentSender<Self>, root: &Self::Root) {
        match msg {
            TreemanCmd::Update {
                label,
                icon,
                severity,
                visible,
            } => {
                root.set_visible(visible);
                self.bar_button.emit(BarButtonInput::SetLabel(label));
                self.bar_button.emit(BarButtonInput::SetIcon(icon));
                force_window_resize(root);

                if self.severity != severity {
                    if let Some(prev) = self.severity {
                        root.remove_css_class(prev);
                    }
                    if let Some(next) = severity {
                        root.add_css_class(next);
                    }
                    self.severity = severity;
                }
            }
        }
    }
}
