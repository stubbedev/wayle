use std::{sync::Arc, time::Duration};

use relm4::ComponentSender;
use tokio_util::sync::CancellationToken;
use wayle_brightness::{BacklightDevice, BrightnessService};
use wayle_config::schemas::{modules::BrightnessConfig, styling::evaluate_thresholds};
use wayle_widgets::{watch, watch_cancellable_throttled};

use super::{BrightnessModule, helpers::average_percentage, messages::BrightnessCmd};

const BRIGHTNESS_THROTTLE: Duration = Duration::from_millis(30);

pub(super) fn spawn_watchers(
    sender: &ComponentSender<BrightnessModule>,
    config: &BrightnessConfig,
    brightness: &Arc<BrightnessService>,
) {
    let devices = brightness.devices.clone();
    watch!(sender, [devices.watch()], |out| {
        let _ = out.send(BrightnessCmd::DevicesChanged(devices.get()));
    });

    let level_icons = config.level_icons.clone();
    let format = config.format.clone();
    watch!(sender, [level_icons.watch(), format.watch()], |out| {
        let _ = out.send(BrightnessCmd::ConfigChanged);
    });

    let thresholds = config.thresholds.clone();
    let threshold_devices = brightness.devices.clone();
    watch!(sender, [thresholds.watch()], |out| {
        if let Some(percentage) = average_percentage(&threshold_devices.get()) {
            let colors = evaluate_thresholds(percentage, &thresholds.get());
            let _ = out.send(BrightnessCmd::UpdateThresholdColors(colors));
        }
    });
}

pub(super) fn spawn_device_watchers(
    sender: &ComponentSender<BrightnessModule>,
    devices: &[Arc<BacklightDevice>],
    token: CancellationToken,
) {
    for device in devices {
        let brightness = device.brightness.clone();
        watch_cancellable_throttled!(
            sender,
            token.clone(),
            BRIGHTNESS_THROTTLE,
            [brightness.watch()],
            |out| {
                let _ = out.send(BrightnessCmd::BrightnessChanged);
            }
        );
    }
}
