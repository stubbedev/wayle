mod battery_section;
mod factory;
mod messages;
mod power_profile;
mod watchers;

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_config::schemas::styling::Size;
use wayle_widgets::prelude::*;

pub(super) use self::factory::Factory;
use self::{
    battery_section::{BatterySection, BatterySectionInit},
    messages::{BatteryDropdownCmd, BatteryDropdownInit},
    power_profile::{PowerProfileInit, PowerProfileSection},
};
use crate::{i18n::t, shell::bar::dropdowns::resolve_dimension};

const BASE_WIDTH: f32 = 382.0;
const BASE_HEIGHT: f32 = 312.0;

pub(crate) struct BatteryDropdown {
    scaled_width: i32,
    scaled_height: i32,
    width_override: Option<Size>,
    height_override: Option<Size>,
    battery_section: Controller<BatterySection>,
    power_profile: Controller<PowerProfileSection>,
}

#[relm4::component(pub(crate))]
impl Component for BatteryDropdown {
    type Init = BatteryDropdownInit;
    type Input = ();
    type Output = ();
    type CommandOutput = BatteryDropdownCmd;

    view! {
        #[root]
        gtk::Popover {
            set_css_classes: &["dropdown", "battery-dropdown"],
            set_has_arrow: false,
            #[watch]
            set_width_request: model.scaled_width,
            #[watch]
            set_height_request: model.scaled_height,

            #[template]
            Dropdown {

                #[template]
                DropdownHeader {
                    #[template_child]
                    icon {
                        set_visible: true,
                        set_icon_name: Some("ld-battery-full-symbolic"),
                    },
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-battery-title"),
                    },
                    #[template_child]
                    actions {
                        set_visible: false,
                    },
                },

                #[template]
                DropdownContent {
                    set_vexpand: true,

                    #[local_ref]
                    battery_section_widget -> gtk::Box {},

                    #[local_ref]
                    power_profile_widget -> gtk::Box {},
                },
            },
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let battery_section = BatterySection::builder()
            .launch(BatterySectionInit {
                battery: init.battery.clone(),
            })
            .detach();

        let power_profile = PowerProfileSection::builder()
            .launch(PowerProfileInit {
                power_profiles: init.power_profiles.clone(),
            })
            .detach();

        let scale = init.config.config().styling.scale.get().value();
        let size = init.config.config().dropdowns.battery.get();
        watchers::spawn(&sender, &init.config);

        let model = Self {
            scaled_width: resolve_dimension(size.width, BASE_WIDTH, scale),
            scaled_height: resolve_dimension(size.height, BASE_HEIGHT, scale),
            width_override: size.width,
            height_override: size.height,
            battery_section,
            power_profile,
        };

        let battery_section_widget = model.battery_section.widget();
        let power_profile_widget = model.power_profile.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update_cmd(
        &mut self,
        msg: BatteryDropdownCmd,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            BatteryDropdownCmd::ScaleChanged(scale) => {
                self.scaled_width = resolve_dimension(self.width_override, BASE_WIDTH, scale);
                self.scaled_height = resolve_dimension(self.height_override, BASE_HEIGHT, scale);
            }
        }
    }
}
