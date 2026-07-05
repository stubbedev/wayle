use std::sync::Arc;

use relm4::ComponentSender;
use tokio_util::sync::CancellationToken;
use wayle_config::schemas::modules::PowerProfilesConfig;
use wayle_core::DeferredService;
use wayle_power_profiles::PowerProfilesService;
use wayle_widgets::{watch, watch_cancellable, watch_deferred};

use super::{PowerProfilesModule, messages::PowerProfilesCmd};

pub fn spawn_service_watcher(
    sender: &ComponentSender<PowerProfilesModule>,
    power_profiles: &DeferredService<PowerProfilesService>,
) {
    watch_deferred!(sender, power_profiles, PowerProfilesCmd::ServiceReady);
}

pub fn spawn_state_watchers(
    sender: &ComponentSender<PowerProfilesModule>,
    token: CancellationToken,
    service: &Arc<PowerProfilesService>,
) {
    let active_profile = service.power_profiles.active_profile.clone();

    watch_cancellable!(sender, token, [active_profile.watch()], |out| {
        let _ = out.send(PowerProfilesCmd::StateChanged);
    });
}

pub fn spawn_config_watchers(
    sender: &ComponentSender<PowerProfilesModule>,
    config: &PowerProfilesConfig,
) {
    let format = config.format.clone();
    let icon_power_saver = config.icon_power_saver.clone();
    let icon_balanced = config.icon_balanced.clone();
    let icon_performance = config.icon_performance.clone();
    let color_power_saver = config.color_power_saver.clone();
    let color_balanced = config.color_balanced.clone();
    let color_performance = config.color_performance.clone();

    watch!(
        sender,
        [
            format.watch(),
            icon_power_saver.watch(),
            icon_balanced.watch(),
            icon_performance.watch(),
            color_power_saver.watch(),
            color_balanced.watch(),
            color_performance.watch()
        ],
        |out| {
            let _ = out.send(PowerProfilesCmd::ConfigChanged);
        }
    );
}
