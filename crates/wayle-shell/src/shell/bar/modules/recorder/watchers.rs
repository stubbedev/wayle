use relm4::ComponentSender;
use wayle_config::schemas::modules::RecorderConfig;
use wayle_widgets::watch;

use super::{RecorderModule, messages::RecorderCmd};
use crate::services::recorder::RecorderState;

pub(super) fn spawn_config_watchers(
    sender: &ComponentSender<RecorderModule>,
    config: &RecorderConfig,
) {
    let icon_idle = config.icon_idle.clone();
    let icon_recording = config.icon_recording.clone();
    let format = config.format.clone();

    watch!(
        sender,
        [icon_idle.watch(), icon_recording.watch(), format.watch()],
        |out| {
            let _ = out.send(RecorderCmd::ConfigChanged);
        }
    );
}

pub(super) fn spawn_state_watchers(
    sender: &ComponentSender<RecorderModule>,
    state: &RecorderState,
) {
    let active = state.active.clone();
    let paused = state.paused.clone();
    let elapsed = state.elapsed_secs.clone();

    watch!(
        sender,
        [active.watch(), paused.watch(), elapsed.watch()],
        |out| {
            let _ = out.send(RecorderCmd::StateChanged);
        }
    );
}
