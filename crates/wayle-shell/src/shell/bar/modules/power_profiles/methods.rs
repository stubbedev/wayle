use std::sync::Arc;

use relm4::{ComponentController, ComponentSender};
use tracing::debug;
use wayle_config::schemas::modules::PowerProfilesConfig;
use wayle_power_profiles::{PowerProfilesService, types::profile::PowerProfile};
use wayle_widgets::prelude::BarButtonInput;

use super::{PowerProfilesModule, helpers, messages::PowerProfilesCmd};

impl PowerProfilesModule {
    pub(super) fn active_profile(&self) -> PowerProfile {
        self.power_profiles
            .get()
            .map(|s| s.power_profiles.active_profile.get())
            .unwrap_or(PowerProfile::Balanced)
    }

    pub(super) fn update_display(&self, config: &PowerProfilesConfig) {
        let profile = self.active_profile();

        self.bar_button
            .emit(BarButtonInput::SetIcon(helpers::select_icon(
                config, profile,
            )));
        self.bar_button
            .emit(BarButtonInput::SetLabel(helpers::format_label(
                &config.format.get(),
                profile,
            )));
        self.bar_button
            .emit(BarButtonInput::SetThresholdColors(helpers::select_colors(
                config, profile,
            )));
    }

    /// Cycle to the next available power profile.
    pub(super) fn cycle_profile(&self, sender: &ComponentSender<Self>) {
        let Some(service) = self.power_profiles.get() else {
            debug!("power-profiles service unavailable, ignoring cycle");
            return;
        };

        let current = service.power_profiles.active_profile.get();
        let available: Vec<PowerProfile> = service
            .power_profiles
            .profiles
            .get()
            .iter()
            .map(|p| p.profile)
            .collect();
        let next = helpers::next_profile(current, &available);

        debug!(?current, ?next, "cycling power profile");

        let service: Arc<PowerProfilesService> = service;
        sender.oneshot_command(async move {
            if let Err(error) = service.power_profiles.set_active_profile(next).await {
                debug!(%error, "failed to set power profile");
            }
            PowerProfilesCmd::StateChanged
        });
    }
}
