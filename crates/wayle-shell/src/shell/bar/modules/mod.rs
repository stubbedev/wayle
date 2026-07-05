//! Aggregates the bar module factories from the wayle-bar-* domain crates.
//!
//! The modules themselves live in their own crates so they compile in
//! parallel; this file is the single place that maps a config
//! [`BarModule`] variant to its factory.

use std::rc::Rc;

use tracing::warn;
use wayle_bar_apps::modules as apps;
use wayle_bar_hardware::modules as hardware;
use wayle_bar_info::modules as info;
use wayle_bar_media::modules as media;
use wayle_bar_network::modules as network;
use wayle_bar_workspaces::modules as workspaces;
use wayle_config::schemas::bar::{BarModule, ModuleRef};
pub(crate) use wayle_shell_core::bar::module_registry::{ModuleFactory, ModuleInstance};
use wayle_widgets::prelude::BarSettings;

use crate::shell::{bar::dropdowns::DropdownRegistry, services::ShellServices};

macro_rules! register_modules {
    ($($variant:ident => $factory:ty),+ $(,)?) => {
        fn create_from_variant(
            module: BarModule,
            settings: &BarSettings,
            services: &ShellServices,
            dropdowns: &Rc<DropdownRegistry>,
            class: Option<String>,
        ) -> Option<ModuleInstance> {
            match module {
                $(BarModule::$variant => <$factory as ModuleFactory>::create(settings, services, dropdowns, class),)+
                _ => {
                    warn!(?module, "module not implemented");
                    None
                }
            }
        }
    };
}

register_modules! {
    Battery => hardware::battery::Factory,
    Bluetooth => network::bluetooth::Factory,
    Brightness => hardware::brightness::Factory,
    Cava => media::cava::Factory,
    Clock => info::clock::Factory,
    Cpu => hardware::cpu::Factory,
    Dashboard => info::dashboard::Factory,
    HyprlandWorkspaces => workspaces::hyprland_workspaces::Factory,
    Hyprsunset => workspaces::hyprsunset::Factory,
    IdleInhibit => hardware::idle_inhibit::Factory,
    KeybindMode => workspaces::keybind_mode::Factory,
    KeyboardInput => workspaces::keyboard_input::Factory,
    Mail => network::mail::Factory,
    MangoWorkspaces => workspaces::mango_workspaces::Factory,
    Media => media::media::Factory,
    Microphone => media::microphone::Factory,
    Netstat => network::netstat::Factory,
    Network => network::network::Factory,
    NiriWorkspaces => workspaces::niri_workspaces::Factory,
    Notifications => apps::notification::Factory,
    Power => hardware::power::Factory,
    PowerProfiles => hardware::power_profiles::Factory,
    Ram => hardware::ram::Factory,
    Recorder => media::recorder::Factory,
    Screenshot => hardware::screenshot::Factory,
    Separator => info::separator::Factory,
    Storage => hardware::storage::Factory,
    SwayWorkspaces => workspaces::sway_workspaces::Factory,
    Systray => apps::systray::Factory,
    Treeman => apps::treeman::Factory,
    Volume => media::volume::Factory,
    Weather => info::weather::Factory,
    WindowTitle => workspaces::window_title::Factory,
    WorldClock => info::world_clock::Factory,
}

pub(crate) fn create_module(
    module_ref: &ModuleRef,
    settings: &BarSettings,
    services: &ShellServices,
    dropdowns: &Rc<DropdownRegistry>,
) -> Option<ModuleInstance> {
    let module = module_ref.module();
    let class = module_ref.class().map(String::from);

    if let Some(id) = module.custom_id() {
        return info::custom::Factory::create_for_id(id, settings, services, dropdowns, class);
    }

    create_from_variant(module.clone(), settings, services, dropdowns, class)
}
