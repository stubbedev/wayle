pub(crate) mod ddc;
pub(crate) mod hotplug;
pub(crate) mod logind;
pub(crate) mod sysfs;
pub(crate) mod types;

use std::{sync::Arc, time::Duration};

use tokio::{sync::mpsc, task::JoinHandle};
use tokio_util::sync::CancellationToken;
use tracing::{debug, info, warn};

use self::{
    ddc::DdcManager,
    types::{BrightnessEvent, Command, CommandReceiver, EventSender},
};
use crate::{
    Error,
    types::{BacklightInfo, BacklightType, DeviceName},
};

pub(crate) fn start(
    initial_devices: Vec<BacklightInfo>,
    external_enabled: bool,
    mut command_rx: CommandReceiver,
    event_tx: EventSender,
    token: CancellationToken,
) -> JoinHandle<Result<(), Error>> {
    tokio::spawn(async move {
        let logind_connection = logind::connect().await;

        info!(
            count = initial_devices.len(),
            "internal backlight devices discovered"
        );

        for device in &initial_devices {
            debug!(
                name = %device.name,
                backlight_type = ?device.backlight_type,
                brightness = device.brightness,
                max = device.max_brightness,
                "backlight device found"
            );

            let _ = event_tx.send(BrightnessEvent::DeviceAdded(device.clone()));
        }

        let _hotplug_handle = hotplug::spawn(event_tx.clone(), token.child_token());

        // DDC/CI enumeration is slow and blocking, so the manager starts empty
        // and is populated by a detached scan. The command loop runs straight
        // away — internal-panel writes are never blocked on the external scan.
        let ddc_manager = Arc::new(DdcManager::empty());

        // DRM hotplug -> DDC re-scan. The sender is kept alive for the whole
        // loop so the channel never closes (its `recv` then simply pends when
        // external support is off and no monitor is spawned).
        let (refresh_tx, mut refresh_rx) = mpsc::unbounded_channel::<()>();
        if external_enabled {
            scan_ddc(&ddc_manager, &event_tx, Duration::ZERO);
            let _drm_handle = hotplug::spawn_drm(refresh_tx.clone(), token.child_token());
        }
        let _refresh_tx_keepalive = refresh_tx;

        loop {
            tokio::select! {
                _ = token.cancelled() => {
                    info!("brightness backend stopping");
                    return Ok(());
                }

                Some(command) = command_rx.recv() => {
                    match command {
                        Command::SetBrightness { name, value, responder } => {
                            let result = set_brightness(
                                &ddc_manager,
                                &logind_connection,
                                &event_tx,
                                &name,
                                value,
                            ).await;

                            let _ = responder.send(result);
                        }
                    }
                }

                Some(()) = refresh_rx.recv() => {
                    // Coalesce the burst of events a single hotplug emits.
                    while refresh_rx.try_recv().is_ok() {}
                    scan_ddc(&ddc_manager, &event_tx, HOTPLUG_SETTLE);
                }
            }
        }
    })
}

/// Delay before a hotplug-triggered scan, letting DRM settle so the monitor's
/// I²C bus is ready before the (slow) probe.
const HOTPLUG_SETTLE: Duration = Duration::from_millis(500);

/// (Re-)scans DDC monitors and emits add/remove events for the difference.
///
/// Runs detached so the slow I²C probe never stalls brightness writes. Used
/// for the initial population (`settle = ZERO`) and for hotplug refreshes
/// (`settle = HOTPLUG_SETTLE`).
fn scan_ddc(ddc_manager: &Arc<DdcManager>, event_tx: &EventSender, settle: Duration) {
    let manager = ddc_manager.clone();
    let event_tx = event_tx.clone();

    tokio::spawn(async move {
        if !settle.is_zero() {
            tokio::time::sleep(settle).await;
        }

        let Ok((added, removed)) =
            tokio::task::spawn_blocking(move || manager.refresh()).await
        else {
            warn!("DDC scan task panicked");
            return;
        };

        for info in added {
            info!(device = %info.name, "external DDC monitor connected");
            let _ = event_tx.send(BrightnessEvent::DeviceAdded(info));
        }
        for name in removed {
            info!(device = %name, "external DDC monitor disconnected");
            let _ = event_tx.send(BrightnessEvent::DeviceRemoved(name));
        }
    });
}

/// Routes a brightness write to DDC (external monitors) or logind/sysfs
/// (internal panels), then reports the resulting state for external monitors,
/// which have no kernel poll source to pick the change up on their own.
async fn set_brightness(
    ddc_manager: &Arc<DdcManager>,
    logind_connection: &Option<zbus::Connection>,
    event_tx: &EventSender,
    name: &str,
    value: u32,
) -> Result<(), Error> {
    if !ddc_manager.contains(name) {
        return logind::set_brightness(logind_connection, name, value).await;
    }

    let manager = ddc_manager.clone();
    let owned_name = name.to_owned();

    let max = tokio::task::spawn_blocking(move || manager.set_brightness(&owned_name, value))
        .await
        .map_err(|_| Error::DdcUnavailable { device: name.to_owned() })??;

    let _ = event_tx.send(BrightnessEvent::DeviceChanged(BacklightInfo {
        name: DeviceName::new(name.to_owned()),
        backlight_type: BacklightType::Ddc,
        brightness: value.min(max),
        max_brightness: max,
    }));

    Ok(())
}
