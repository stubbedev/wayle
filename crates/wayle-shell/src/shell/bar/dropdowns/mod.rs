mod audio;
mod battery;
mod bluetooth;
mod brightness;
mod calendar;
mod dashboard;
mod mail;
mod media;
mod network;
mod notification;
mod recorder;
mod registry;
mod weather;

use wayle_config::schemas::styling::Size;

pub(crate) use self::registry::{
    DropdownFactory, DropdownInstance, DropdownRegistry, dispatch_click, dispatch_click_widget,
    require_service,
};
use crate::shell::services::ShellServices;

/// Resolves a dropdown width/height override to a pixel request.
///
/// `None` keeps the built-in default (`base * scale`). A [`Size::Scale`]
/// multiplies the base before scaling; a [`Size::Px`] is an absolute length
/// that ignores the scale.
pub(crate) fn resolve_dimension(override_: Option<Size>, base: f32, scale: f32) -> i32 {
    match override_ {
        Some(size) => size.resolve_px(base, scale).round() as i32,
        None => (base * scale).round() as i32,
    }
}

/// Resolves an optional height override for dropdowns that otherwise size their
/// height to content.
///
/// Returns `-1` (GTK's "natural size" request) when no override applies. Only
/// an absolute [`Size::Px`] takes effect, since there is no base height to
/// scale a multiplier against.
pub(crate) fn resolve_content_height(override_: Option<Size>) -> i32 {
    match override_ {
        Some(Size::Px(px)) => px.round() as i32,
        Some(Size::Scale(_)) | None => -1,
    }
}

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
    "audio" => audio::Factory,
    "battery" => battery::Factory,
    "bluetooth" => bluetooth::Factory,
    "brightness" => brightness::Factory,
    "calendar" => calendar::Factory,
    "dashboard" => dashboard::Factory,
    "mail" => mail::Factory,
    "media" => media::Factory,
    "network" => network::Factory,
    "notification" => notification::Factory,
    "recorder" => recorder::Factory,
    "weather" => weather::Factory,
}
