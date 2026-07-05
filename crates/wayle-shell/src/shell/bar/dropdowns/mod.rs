//! Aggregates the dropdown factories from the wayle-bar-* domain crates.
//!
//! The dropdowns live next to their bar modules in the domain crates; this
//! file is the single place that maps a dropdown name to its factory, and it
//! injects that mapping into the shared [`DropdownRegistry`].

use wayle_bar_apps::dropdowns as apps;
use wayle_bar_hardware::dropdowns as hardware;
use wayle_bar_info::dropdowns as info;
use wayle_bar_media::dropdowns as media;
use wayle_bar_network::dropdowns as network;
pub(crate) use wayle_shell_core::bar::dropdown_registry::{
    DropdownFactory, DropdownInstance, DropdownRegistry,
};

use crate::shell::services::ShellServices;

macro_rules! register_dropdowns {
    ($($name:literal => $factory:ty),+ $(,)?) => {
        pub(crate) const DROPDOWN_NAMES: &[&str] = &[$($name),+];

        pub(crate) fn create(
            name: &str,
            services: &ShellServices,
        ) -> Option<DropdownInstance> {
            match name {
                $($name => <$factory as DropdownFactory>::create(services),)+
                _ => {
                    tracing::warn!(dropdown = name, "unknown dropdown type");
                    None
                }
            }
        }
    };
}

register_dropdowns! {
    "audio" => media::audio::Factory,
    "battery" => hardware::battery::Factory,
    "bluetooth" => network::bluetooth::Factory,
    "brightness" => hardware::brightness::Factory,
    "calendar" => info::calendar::Factory,
    "dashboard" => info::dashboard::Factory,
    "mail" => network::mail::Factory,
    "media" => media::media::Factory,
    "network" => network::network::Factory,
    "notification" => apps::notification::Factory,
    "recorder" => media::recorder::Factory,
    "treeman" => apps::treeman::Factory,
    "weather" => info::weather::Factory,
}
