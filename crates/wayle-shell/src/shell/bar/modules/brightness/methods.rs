use relm4::ComponentController;
use wayle_config::schemas::{modules::BrightnessConfig, styling::evaluate_thresholds};
use wayle_widgets::prelude::BarButtonInput;

use super::{
    BrightnessModule,
    helpers::{IconContext, average_percentage, format_label, select_icon},
};

/// Placeholder label shown when no monitors are present.
const NO_DEVICE_LABEL: &str = "--%";

impl BrightnessModule {
    /// Recomputes the label, icon, and threshold colors from the average
    /// brightness across all monitors (internal panels and external DDC).
    pub(super) fn refresh_display(&self, config: &BrightnessConfig) {
        match average_percentage(&self.devices) {
            Some(percentage) => {
                self.update_display(config, percentage);
                self.apply_thresholds(config, percentage);
            }
            None => {
                self.bar_button
                    .emit(BarButtonInput::SetLabel(String::from(NO_DEVICE_LABEL)));
                if let Some(icon) = config.level_icons.get().first() {
                    self.bar_button.emit(BarButtonInput::SetIcon(icon.clone()));
                }
            }
        }
    }

    fn update_display(&self, config: &BrightnessConfig, percentage: f64) {
        let label = format_label(&config.format.get(), percentage);
        self.bar_button.emit(BarButtonInput::SetLabel(label));

        let icons = config.level_icons.get();
        let icon = select_icon(&IconContext {
            percentage,
            level_icons: &icons,
        });
        self.bar_button.emit(BarButtonInput::SetIcon(icon));
    }

    fn apply_thresholds(&self, config: &BrightnessConfig, percentage: f64) {
        let colors = evaluate_thresholds(percentage, &config.thresholds.get());
        self.bar_button
            .emit(BarButtonInput::SetThresholdColors(colors));
    }
}
