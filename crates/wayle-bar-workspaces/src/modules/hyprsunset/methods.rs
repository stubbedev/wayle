use chrono::Utc;
use relm4::prelude::*;
use tracing::debug;
use wayle_config::schemas::modules::HyprsunsetConfig;
use wayle_widgets::prelude::BarButtonInput;

use super::{
    HyprsunsetModule,
    helpers::{self, LabelContext},
    messages::HyprsunsetCmd,
    solar::{self, Phase},
};

impl HyprsunsetModule {
    pub fn toggle_filter(&self, sender: &ComponentSender<Self>, config: &HyprsunsetConfig) {
        let enabled = self.enabled;
        let temp = config.temperature.get();
        let gamma = config.gamma.get();

        debug!(current_enabled = enabled, "toggle_filter called");

        sender.oneshot_command(async move {
            if enabled {
                debug!("stopping hyprsunset");
                let _ = helpers::stop().await;
                HyprsunsetCmd::StateChanged(None)
            } else {
                debug!(temp, gamma, "starting hyprsunset");
                let _ = helpers::start(temp, gamma).await;
                HyprsunsetCmd::StateChanged(Some(helpers::HyprsunsetState { temp, gamma }))
            }
        });
    }

    /// Re-evaluate the sunrise/sunset auto-schedule and drive the filter.
    ///
    /// Night → filter on (at the configured temperature/gamma); day → off. A
    /// manual toggle sets [`Self::manual_override`], which suppresses automatic
    /// changes until the next sunrise/sunset boundary, then resumes.
    pub fn evaluate_schedule(&mut self, sender: &ComponentSender<Self>, config: &HyprsunsetConfig) {
        if !config.auto_schedule.get() {
            // Reset so re-enabling re-applies from a clean slate.
            self.auto_phase = None;
            self.manual_override = false;
            return;
        }

        // GeoClue location wins when available; otherwise the configured coords.
        let (lat, lng) = self
            .geo_location
            .unwrap_or((config.latitude.get(), config.longitude.get()));
        let phase = solar::phase_at(Utc::now(), lat, lng);
        let phase_changed = self.auto_phase != Some(phase);
        self.auto_phase = Some(phase);

        if phase_changed {
            // Crossing sunrise/sunset hands control back to the schedule.
            self.manual_override = false;
        }

        if self.manual_override {
            return;
        }

        let want_on = phase == Phase::Night;
        if want_on == self.enabled {
            return;
        }

        if want_on {
            let temp = config.temperature.get();
            let gamma = config.gamma.get();
            debug!(temp, gamma, "auto-schedule: night, enabling filter");
            sender.oneshot_command(async move {
                let _ = helpers::start(temp, gamma).await;
                HyprsunsetCmd::StateChanged(Some(helpers::HyprsunsetState { temp, gamma }))
            });
        } else {
            debug!("auto-schedule: day, disabling filter");
            sender.oneshot_command(async move {
                let _ = helpers::stop().await;
                HyprsunsetCmd::StateChanged(None)
            });
        }
    }

    pub fn update_display(&self, config: &HyprsunsetConfig) {
        let icon =
            helpers::select_icon(self.enabled, &config.icon_off.get(), &config.icon_on.get());
        self.bar_button.emit(BarButtonInput::SetIcon(icon));

        let label = helpers::build_label(&LabelContext {
            format: &config.format.get(),
            temp: self.current_temp,
            gamma: self.current_gamma,
            config_temp: config.temperature.get(),
            config_gamma: config.gamma.get(),
            enabled: self.enabled,
        });
        self.bar_button.emit(BarButtonInput::SetLabel(label));
    }
}
