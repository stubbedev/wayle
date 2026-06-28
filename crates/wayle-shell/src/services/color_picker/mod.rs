//! In-process colour-picker overlay bridge.
//!
//! The magnifier-loupe picker lives on the GTK thread as the [`ColorPicker`]
//! component. The screenshot service asks for a colour through
//! [`request_color`], handing over the frozen per-output frames it captured
//! (the screenshot host owns the wlroots capture path). No D-Bus is involved —
//! both run in the shell process.
//!
//! [`ColorPicker`]: crate::shell::color_picker::ColorPicker

use std::{collections::HashMap, sync::OnceLock};

use relm4::Sender;
use tokio::sync::oneshot;
use tracing::warn;

use crate::shell::color_picker::{ColorPickerInput, FrameData};

/// GTK-thread sender into the picker component. Set once the shell UI exists.
static PICKER_SENDER: OnceLock<Sender<ColorPickerInput>> = OnceLock::new();

/// Records the picker component's input sender. Called once during shell init;
/// later calls are ignored.
pub(crate) fn register_sender(sender: Sender<ColorPickerInput>) {
    if PICKER_SENDER.set(sender).is_err() {
        warn!("color picker sender already registered");
    }
}

/// Shows the colour picker over the given frozen per-output frames and awaits
/// the user's pick. Returns the sRGB `(r, g, b)` in `[0, 1]`, or `None` on
/// cancel / when the shell UI has not registered its sender yet.
pub(crate) async fn request_color(
    frames: HashMap<String, FrameData>,
) -> Option<(f64, f64, f64)> {
    let sender = PICKER_SENDER.get()?.clone();
    let (reply_tx, reply_rx) = oneshot::channel();
    sender.emit(ColorPickerInput::Show {
        reply: reply_tx,
        frames,
    });
    reply_rx.await.unwrap_or(None)
}
