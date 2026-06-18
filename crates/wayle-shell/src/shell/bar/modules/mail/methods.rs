use relm4::{ComponentController, gtk, gtk::prelude::WidgetExt};
use wayle_config::schemas::modules::MailConfig;
use wayle_widgets::prelude::BarButtonInput;

use super::{MailModule, helpers};

impl MailModule {
    pub(super) fn update_display(&self, config: &MailConfig, root: &gtk::Box) {
        self.bar_button
            .emit(BarButtonInput::SetIcon(config.icon_name.get()));
        self.bar_button
            .emit(BarButtonInput::SetLabel(helpers::format_label(
                &config.format.get(),
                self.count,
            )));

        let visible = self.count > 0 || !config.hide_when_zero.get();
        root.set_visible(visible);
    }
}
