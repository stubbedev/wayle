use relm4::ComponentController;
use wayle_widgets::prelude::BarButtonInput;

use super::{
    RecorderModule,
    helpers::{self, LabelContext},
};

impl RecorderModule {
    pub(super) fn update_display(&self, config: &wayle_config::schemas::modules::RecorderConfig) {
        let active = self.state.active.get();
        let paused = self.state.paused.get();
        let preparing = self.state.preparing.get();

        let icon = helpers::select_icon(
            active,
            preparing,
            paused,
            &config.icon_idle.get(),
            &config.icon_recording.get(),
            &config.icon_paused.get(),
        );
        self.bar_button.emit(BarButtonInput::SetIcon(icon));

        let label = helpers::build_label(
            &config.format.get(),
            &LabelContext {
                active,
                paused,
                elapsed_secs: self.state.elapsed_secs.get(),
            },
        );
        self.bar_button.emit(BarButtonInput::SetLabel(label));
    }
}
