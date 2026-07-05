use std::sync::Arc;

use relm4::ComponentSender;
use wayle_config::ConfigService;
use wayle_widgets::watch;

use super::{MediaDropdown, messages::MediaDropdownCmd};

pub fn spawn(sender: &ComponentSender<MediaDropdown>, config: &Arc<ConfigService>) {
    let scale = config.config().styling.scale.clone();
    watch!(sender, [scale.watch()], |out| {
        let _ = out.send(MediaDropdownCmd::ScaleChanged(scale.get().value()));
    });
}
