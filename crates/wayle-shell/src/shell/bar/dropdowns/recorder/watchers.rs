use std::sync::Arc;

use relm4::ComponentSender;
use wayle_config::ConfigService;
use wayle_widgets::watch;

use super::{RecorderDropdown, messages::RecorderDropdownCmd};
use crate::services::recorder::RecorderState;

pub(super) fn spawn(
    sender: &ComponentSender<RecorderDropdown>,
    config: &Arc<ConfigService>,
    state: &RecorderState,
) {
    let scale = config.config().styling.scale.clone();
    watch!(sender, [scale.watch()], |out| {
        let _ = out.send(RecorderDropdownCmd::ScaleChanged(scale.get().value()));
    });

    let active = state.active.clone();
    let paused = state.paused.clone();
    let elapsed = state.elapsed_secs.clone();
    watch!(
        sender,
        [active.watch(), paused.watch(), elapsed.watch()],
        |out| {
            let _ = out.send(RecorderDropdownCmd::StateChanged);
        }
    );
}
