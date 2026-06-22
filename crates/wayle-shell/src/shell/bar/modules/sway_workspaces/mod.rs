//! sway workspace switcher bar module.

mod button;
mod factory;
mod filtering;
mod helpers;
mod messages;
mod methods;
mod styling;
mod watchers;

use std::{rc::Rc, sync::Arc, time::Duration};

use gtk::prelude::*;
use relm4::{factory::FactoryVecDeque, prelude::*};
use tokio_util::sync::CancellationToken;
use wayle_config::ConfigService;
use wayle_sway::SwayService;
use wayle_widgets::{prelude::BarSettings, utils::force_window_resize};

use self::button::{SwayWorkspaceButton, SwayWorkspaceButtonOutput};
pub(crate) use self::{
    factory::Factory,
    messages::{SwayWorkspacesCmd, SwayWorkspacesInit, SwayWorkspacesMsg},
};
use crate::shell::{bar::dropdowns::DropdownRegistry, helpers::COMPONENT_CSS_PRIORITY};

pub(super) const BLINK_INTERVAL: Duration = Duration::from_millis(500);

pub(crate) struct SwayWorkspaces {
    pub(super) sway: Arc<SwayService>,
    pub(super) config: Arc<ConfigService>,
    pub(super) settings: BarSettings,
    pub(super) dropdowns: Rc<DropdownRegistry>,
    pub(super) css_provider: gtk::CssProvider,
    pub(super) buttons: FactoryVecDeque<SwayWorkspaceButton>,
    pub(super) blink_on: bool,
    pub(super) blink_token: Option<CancellationToken>,
    pub(super) urgent_present: bool,
}

#[relm4::component(pub(crate))]
impl Component for SwayWorkspaces {
    type Init = SwayWorkspacesInit;
    type Input = SwayWorkspacesMsg;
    type Output = ();
    type CommandOutput = SwayWorkspacesCmd;

    view! {
        gtk::Box {
            add_css_class: "workspaces",
            add_css_class: "sway",
            #[watch]
            set_orientation: model.orientation(),
            #[watch]
            set_hexpand: model.is_vertical(),
            #[watch]
            set_vexpand: !model.is_vertical(),
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let config = init.config.config();
        let workspaces_config = &config.modules.sway_workspaces;
        let theme_provider = config.styling.theme_provider.clone();
        let bar_scale = config.bar.scale.clone();

        watchers::spawn_watchers(
            &sender,
            workspaces_config,
            init.sway.clone(),
            theme_provider,
            bar_scale,
            &init.settings,
        );

        let css_provider = gtk::CssProvider::new();
        gtk::style_context_add_provider_for_display(
            &root.display(),
            &css_provider,
            COMPONENT_CSS_PRIORITY,
        );

        let buttons = FactoryVecDeque::builder().launch(root.clone()).forward(
            sender.input_sender(),
            |output| match output {
                SwayWorkspaceButtonOutput::LeftClick(id) => SwayWorkspacesMsg::LeftClick(id),
                SwayWorkspaceButtonOutput::MiddleClick(id) => SwayWorkspacesMsg::MiddleClick(id),
                SwayWorkspaceButtonOutput::RightClick(id) => SwayWorkspacesMsg::RightClick(id),
                SwayWorkspaceButtonOutput::ScrollUp => SwayWorkspacesMsg::ScrollUp,
                SwayWorkspaceButtonOutput::ScrollDown => SwayWorkspacesMsg::ScrollDown,
            },
        );

        let mut model = Self {
            sway: init.sway,
            config: init.config,
            settings: init.settings,
            dropdowns: init.dropdowns,
            css_provider,
            buttons,
            blink_on: false,
            blink_token: None,
            urgent_present: false,
        };
        styling::apply_styling(&model.css_provider, &model.config, &model.settings);
        model.rebuild_buttons();
        model.sync_blink(&sender);

        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, _sender: ComponentSender<Self>, _root: &Self::Root) {
        let ws_config = &self.config.config().modules.sway_workspaces;

        match msg {
            SwayWorkspacesMsg::LeftClick(id) => {
                self.dispatch_click_action(ws_config.left_click.get(), id);
            }
            SwayWorkspacesMsg::MiddleClick(id) => {
                self.dispatch_click_action(ws_config.middle_click.get(), id);
            }
            SwayWorkspacesMsg::RightClick(id) => {
                self.dispatch_click_action(ws_config.right_click.get(), id);
            }
            SwayWorkspacesMsg::ScrollUp => {
                self.dispatch_scroll_action(ws_config.scroll_up.get());
            }
            SwayWorkspacesMsg::ScrollDown => {
                self.dispatch_scroll_action(ws_config.scroll_down.get());
            }
        }
    }

    fn update_cmd(
        &mut self,
        msg: SwayWorkspacesCmd,
        sender: ComponentSender<Self>,
        root: &Self::Root,
    ) {
        match msg {
            SwayWorkspacesCmd::WorkspacesChanged => {
                self.rebuild_buttons();
                self.sync_blink(&sender);
                force_window_resize(root);
            }
            SwayWorkspacesCmd::ConfigChanged => {
                styling::apply_styling(&self.css_provider, &self.config, &self.settings);
                self.rebuild_buttons();
                self.sync_blink(&sender);
                force_window_resize(root);
            }
            SwayWorkspacesCmd::BlinkTick => {
                self.blink_on = !self.blink_on;
                self.rebuild_buttons();
            }
        }
    }
}

impl Drop for SwayWorkspaces {
    fn drop(&mut self) {
        gtk::style_context_remove_provider_for_display(
            &self.buttons.widget().display(),
            &self.css_provider,
        );
    }
}
