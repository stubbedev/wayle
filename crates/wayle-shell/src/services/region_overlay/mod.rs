//! In-process region-selection overlay bridge.
//!
//! The slurp-like overlay lives on the GTK thread as the [`RegionOverlay`]
//! component. Both the share picker and the screenshot service ask for a
//! region through [`request_region`], which forwards a request to the
//! component and awaits the user's drag selection. No D-Bus is involved — both
//! callers run in the shell process.
//!
//! [`RegionOverlay`]: crate::shell::region_overlay::RegionOverlay

use std::sync::OnceLock;

use relm4::Sender;
use tokio::sync::oneshot;
use tracing::warn;

use crate::shell::region_overlay::{RegionOverlayInput, RegionSelection};

/// GTK-thread sender into the overlay component. Set once the shell UI exists.
static OVERLAY_SENDER: OnceLock<Sender<RegionOverlayInput>> = OnceLock::new();

/// Records the overlay component's input sender. Called once during shell
/// init; later calls are ignored.
pub(crate) fn register_sender(sender: Sender<RegionOverlayInput>) {
    if OVERLAY_SENDER.set(sender).is_err() {
        warn!("region overlay sender already registered");
    }
}

/// Shows the region overlay and awaits the user's selection.
///
/// Returns `None` if the user cancels (Escape), makes no selection, or the
/// shell UI has not registered its sender yet.
pub(crate) async fn request_region() -> Option<RegionSelection> {
    let sender = OVERLAY_SENDER.get()?.clone();
    let (reply_tx, reply_rx) = oneshot::channel();
    sender.emit(RegionOverlayInput::Show { reply: reply_tx });
    reply_rx.await.unwrap_or(None)
}
