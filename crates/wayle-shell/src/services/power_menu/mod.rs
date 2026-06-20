//! In-process power-menu bridge.
//!
//! The power menu lives on the GTK thread as the [`PowerMenu`] component. The
//! power bar button opens it via [`show`] (routed from the `:menu` click
//! command), with no D-Bus or subprocess.
//!
//! [`PowerMenu`]: crate::shell::power_menu::PowerMenu

use std::sync::OnceLock;

use relm4::Sender;
use tracing::warn;

use crate::shell::power_menu::PowerMenuInput;

/// GTK-thread sender into the power menu. Set once the shell UI exists.
static MENU_SENDER: OnceLock<Sender<PowerMenuInput>> = OnceLock::new();

/// Records the power menu's input sender. Called once during shell init.
pub(crate) fn register_sender(sender: Sender<PowerMenuInput>) {
    if MENU_SENDER.set(sender).is_err() {
        warn!("power menu sender already registered");
    }
}

/// Opens the power menu. Returns `false` if the shell UI is not ready.
pub(crate) fn show() -> bool {
    let Some(sender) = MENU_SENDER.get() else {
        return false;
    };
    sender.emit(PowerMenuInput::Show);
    true
}
