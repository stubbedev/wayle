mod factory;
mod geoclue;
mod helpers;
mod messages;
mod methods;
mod solar;
mod watchers;

use std::{rc::Rc, sync::Arc};

use gtk::prelude::*;
use relm4::prelude::*;
use tracing::debug;
use wayle_config::{ClickAction, ConfigProperty, ConfigService, schemas::styling::CssToken};
use wayle_widgets::prelude::{
    BarButton, BarButtonBehavior, BarButtonColors, BarButtonInit, BarButtonOutput,
};

use self::solar::Phase;
pub(crate) use self::{
    factory::Factory,
    messages::{HyprsunsetCmd, HyprsunsetInit, HyprsunsetMsg},
};
use crate::shell::bar::dropdowns::{self, DropdownRegistry};

pub(crate) struct HyprsunsetModule {
    bar_button: Controller<BarButton>,
    config: Arc<ConfigService>,
    enabled: bool,
    current_temp: u32,
    current_gamma: u32,
    dropdowns: Rc<DropdownRegistry>,
    /// Last solar phase applied by the auto-schedule (None while disabled).
    auto_phase: Option<Phase>,
    /// A manual toggle is overriding the auto-schedule until the next boundary.
    manual_override: bool,
    /// Location resolved via GeoClue; preferred over configured lat/long.
    geo_location: Option<(f64, f64)>,
}

#[relm4::component(pub(crate))]
impl Component for HyprsunsetModule {
    type Init = HyprsunsetInit;
    type Input = HyprsunsetMsg;
    type Output = ();
    type CommandOutput = HyprsunsetCmd;

    view! {
        gtk::Box {
            add_css_class: "hyprsunset",

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
        let config = config_service.config().modules.hyprsunset.clone();

        let bar_button = BarButton::builder()
            .launch(BarButtonInit {
                icon: config.icon_off.get().clone(),
                label: String::new(),
                tooltip: None,
                colors: BarButtonColors {
                    icon_color: config.icon_color.clone(),
                    label_color: config.label_color.clone(),
                    icon_background: config.icon_bg_color.clone(),
                    button_background: config.button_bg_color.clone(),
                    border_color: config.border_color.clone(),
                    auto_icon_color: CssToken::Yellow,
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
                BarButtonOutput::LeftClick => HyprsunsetMsg::LeftClick,
                BarButtonOutput::RightClick => HyprsunsetMsg::RightClick,
                BarButtonOutput::MiddleClick => HyprsunsetMsg::MiddleClick,
                BarButtonOutput::ScrollUp => HyprsunsetMsg::ScrollUp,
                BarButtonOutput::ScrollDown => HyprsunsetMsg::ScrollDown,
            });

        watchers::spawn_config_watchers(&sender, &config);
        watchers::spawn_state_watcher(&sender);
        watchers::spawn_schedule_watcher(&sender);
        watchers::spawn_schedule_config_watcher(&sender, &config);
        watchers::spawn_location_watcher(&sender, &config);

        let model = Self {
            bar_button,
            config: config_service,
            enabled: false,
            current_temp: config.temperature.get(),
            current_gamma: config.gamma.get(),
            dropdowns: init.dropdowns,
            auto_phase: None,
            manual_override: false,
            geo_location: None,
        };
        let bar_button = model.bar_button.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>, _root: &Self::Root) {
        let config = &self.config.config().modules.hyprsunset;

        let action = match msg {
            HyprsunsetMsg::LeftClick => {
                let action = config.left_click.get();
                if matches!(&action, ClickAction::Shell(s) if s == ":toggle") {
                    // Under auto-schedule, a manual toggle overrides the schedule
                    // until the next sunrise/sunset boundary.
                    if config.auto_schedule.get() {
                        self.manual_override = true;
                    }
                    self.toggle_filter(&sender, config);
                    return;
                }
                action
            }
            HyprsunsetMsg::RightClick => config.right_click.get(),
            HyprsunsetMsg::MiddleClick => config.middle_click.get(),
            HyprsunsetMsg::ScrollUp => config.scroll_up.get(),
            HyprsunsetMsg::ScrollDown => config.scroll_down.get(),
        };

        dropdowns::dispatch_click(&action, &self.dropdowns, &self.bar_button);
    }

    fn update_cmd(
        &mut self,
        msg: HyprsunsetCmd,
        sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        let config = self.config.config().modules.hyprsunset.clone();

        match msg {
            HyprsunsetCmd::ConfigChanged => {
                self.update_display(&config);
            }
            HyprsunsetCmd::StateChanged(state) => {
                let enabled = state.is_some();
                let (temp, gamma) = state
                    .map(|s| (s.temp, s.gamma))
                    .unwrap_or((config.temperature.get(), config.gamma.get()));

                if self.enabled != enabled
                    || self.current_temp != temp
                    || self.current_gamma != gamma
                {
                    debug!(enabled, temp, gamma, "hyprsunset state changed");
                    self.enabled = enabled;
                    self.current_temp = temp;
                    self.current_gamma = gamma;
                    self.update_display(&config);
                }
            }
            HyprsunsetCmd::TickSchedule => {
                self.evaluate_schedule(&sender, &config);
            }
            HyprsunsetCmd::LocationResolved(lat, lng) => {
                if self.geo_location != Some((lat, lng)) {
                    debug!(lat, lng, "geoclue location resolved");
                    self.geo_location = Some((lat, lng));
                    self.evaluate_schedule(&sender, &config);
                }
            }
        }
    }
}
