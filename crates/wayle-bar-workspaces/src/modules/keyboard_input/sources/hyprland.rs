//! Hyprland implementation of [`KeyboardLayoutSource`].
//!
//! Hyprland's `events()` returns a stream tied to `&self` under Rust 2024
//! capture rules, so we forward events through a spawned task + unbounded
//! channel to produce a `'static` stream. The task owns its own [`Arc`] of
//! the service and ends when the consumer drops the stream.

use std::sync::Arc;

use futures::{StreamExt, stream::BoxStream};
use tokio::{runtime::Handle, sync::mpsc};
use tokio_stream::wrappers::UnboundedReceiverStream;
use tracing::warn;
use wayle_hyprland::{DeviceInfo, HyprlandEvent, HyprlandService};

use super::{CurrentLayout, KeyboardLayoutSource};

pub struct HyprlandKeyboardLayoutSource {
    service: Arc<HyprlandService>,
}

impl HyprlandKeyboardLayoutSource {
    pub fn new(service: Arc<HyprlandService>) -> Self {
        Self { service }
    }
}

impl KeyboardLayoutSource for HyprlandKeyboardLayoutSource {
    fn snapshot(&self) -> Option<CurrentLayout> {
        let runtime = Handle::current();
        match runtime.block_on(self.service.devices()) {
            Ok(devices) => main_keyboard_layout(&devices).map(|label| CurrentLayout {
                label: label.to_string(),
            }),
            Err(err) => {
                warn!(error = %err, "cannot read hyprland keyboard devices");
                None
            }
        }
    }

    fn changes(&self) -> BoxStream<'static, Option<CurrentLayout>> {
        let service = Arc::clone(&self.service);
        let (tx, rx) = mpsc::unbounded_channel();

        tokio::spawn(async move {
            let mut events = service.events();
            while let Some(event) = events.next().await {
                let HyprlandEvent::ActiveLayout { .. } = event else {
                    continue;
                };

                // The payload is not trustworthy: Hyprland emits the event for
                // *every* keyboard, including the throwaway
                // `hl-virtual-keyboard-*` that wtype and friends create for a
                // single synthetic keystroke — whose teardown reports the layout
                // as `none`. Re-read the main keyboard so this agrees with
                // `snapshot()` instead of latching onto a foreign device.
                let layout = match service.devices().await {
                    Ok(devices) => main_keyboard_layout(&devices).map(|label| CurrentLayout {
                        label: label.to_string(),
                    }),
                    Err(err) => {
                        warn!(error = %err, "cannot read hyprland keyboard devices");
                        continue;
                    }
                };

                if tx.send(layout).is_err() {
                    return;
                }
            }
        });

        Box::pin(UnboundedReceiverStream::new(rx))
    }
}

fn main_keyboard_layout(devices: &DeviceInfo) -> Option<&str> {
    devices
        .keyboards
        .iter()
        .find(|keyboard| keyboard.main)
        .map(|keyboard| keyboard.active_keymap.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Trimmed `hyprctl devices -j`: the transient keyboard wtype spawns, listed
    /// before the real one, reporting no layout at all.
    fn devices(virtual_first: bool) -> DeviceInfo {
        let virtual_kb = r#"{
            "address": "0x1",
            "name": "hl-virtual-keyboard-wtype",
            "rules": "",
            "model": "",
            "layout": "",
            "variant": "",
            "options": "",
            "active_layout_index": 0,
            "active_keymap": "none",
            "capsLock": false,
            "numLock": false,
            "main": false
        }"#;
        let main_kb = r#"{
            "address": "0x2",
            "name": "keychron-keychron-k3",
            "rules": "",
            "model": "",
            "layout": "us,dk",
            "variant": "",
            "options": "grp:toggle",
            "active_layout_index": 1,
            "active_keymap": "Danish",
            "capsLock": false,
            "numLock": false,
            "main": true
        }"#;
        let keyboards = if virtual_first {
            format!("[{virtual_kb},{main_kb}]")
        } else {
            format!("[{main_kb}]")
        };
        serde_json::from_str(&format!(
            r#"{{"mice":[],"keyboards":{keyboards},"tablets":[],"touch":[],"switches":[]}}"#
        ))
        .expect("device fixture parses")
    }

    #[test]
    fn ignores_virtual_keyboards() {
        assert_eq!(main_keyboard_layout(&devices(true)), Some("Danish"));
        assert_eq!(main_keyboard_layout(&devices(false)), Some("Danish"));
    }

    #[test]
    fn no_main_keyboard_yields_no_layout() {
        let mut devices = devices(true);
        devices.keyboards.retain(|keyboard| !keyboard.main);

        assert_eq!(main_keyboard_layout(&devices), None);
    }
}
