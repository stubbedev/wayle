use std::{os::fd::AsFd, slice, time::Duration};

use nix::poll::{PollFd, PollFlags, PollTimeout, poll};
use tokio::{sync::mpsc::UnboundedSender, task::JoinHandle};
use tokio_util::sync::CancellationToken;
use tracing::{debug, info, warn};

use super::{
    sysfs,
    types::{BrightnessEvent, EventSender},
};
use crate::Error;

const BACKLIGHT_SUBSYSTEM: &str = "backlight";
const DRM_SUBSYSTEM: &str = "drm";
const POLL_TIMEOUT: Duration = Duration::from_secs(2);

/// Watches for backlight device add/remove via udev.
pub(crate) fn spawn(event_tx: EventSender, token: CancellationToken) -> JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        if let Err(err) = monitor_loop(&event_tx, &token) {
            warn!(error = %err, "udev backlight monitor stopped");
        }
    })
}

/// Watches the DRM subsystem for monitor hotplug (connect/disconnect) and
/// pulses `refresh_tx` so the backend re-scans DDC/CI external monitors.
///
/// DRM signals a display change as a `change` uevent with `HOTPLUG=1` on the
/// card device; the i2c buses themselves are static, so this is the reliable
/// trigger for external-monitor add/remove.
pub(crate) fn spawn_drm(
    refresh_tx: UnboundedSender<()>,
    token: CancellationToken,
) -> JoinHandle<()> {
    tokio::task::spawn_blocking(move || {
        if let Err(err) = drm_monitor_loop(&refresh_tx, &token) {
            warn!(error = %err, "udev drm monitor stopped");
        }
    })
}

fn monitor_loop(event_tx: &EventSender, token: &CancellationToken) -> Result<(), Error> {
    let socket = match build_monitor_socket(BACKLIGHT_SUBSYSTEM) {
        Some(socket) => socket,
        None => return Ok(()),
    };

    info!("udev backlight hotplug monitor started");

    let timeout_ms = POLL_TIMEOUT.as_millis() as u16;

    loop {
        if token.is_cancelled() {
            debug!("udev monitor cancelled");
            return Ok(());
        }

        let fd = socket.as_fd();
        let mut poll_fd = PollFd::new(fd, PollFlags::POLLIN);

        match poll(slice::from_mut(&mut poll_fd), PollTimeout::from(timeout_ms)) {
            Ok(0) => continue,
            Err(_) => continue,
            Ok(_) => {}
        }

        for event in socket.iter() {
            let Some(device_name) = event.sysname().to_str() else {
                continue;
            };

            handle_event(device_name, &event, event_tx);
        }
    }
}

fn drm_monitor_loop(
    refresh_tx: &UnboundedSender<()>,
    token: &CancellationToken,
) -> Result<(), Error> {
    let socket = match build_monitor_socket(DRM_SUBSYSTEM) {
        Some(socket) => socket,
        None => return Ok(()),
    };

    info!("udev drm hotplug monitor started");

    let timeout_ms = POLL_TIMEOUT.as_millis() as u16;

    loop {
        if token.is_cancelled() {
            debug!("udev drm monitor cancelled");
            return Ok(());
        }

        let fd = socket.as_fd();
        let mut poll_fd = PollFd::new(fd, PollFlags::POLLIN);

        match poll(slice::from_mut(&mut poll_fd), PollTimeout::from(timeout_ms)) {
            Ok(0) => continue,
            Err(_) => continue,
            Ok(_) => {}
        }

        for event in socket.iter() {
            if is_drm_hotplug(&event) {
                debug!("drm hotplug event, requesting DDC re-scan");
                // Receiver gone => backend stopped; end the loop.
                if refresh_tx.send(()).is_err() {
                    return Ok(());
                }
            }
        }
    }
}

/// True only for a DRM connector hotplug. The kernel marks these as a `change`
/// action carrying `HOTPLUG=1`; other `change` events (mode sets, DPMS, EDID
/// property updates) omit it, so requiring the flag avoids needless DDC
/// re-scans. All DDC/CI-capable drivers (i915, amdgpu, nouveau) set it.
fn is_drm_hotplug(event: &udev::Event) -> bool {
    event.action().and_then(|action| action.to_str()) == Some("change")
        && event.property_value("HOTPLUG").and_then(|v| v.to_str()) == Some("1")
}

fn handle_event(device_name: &str, event: &udev::Event, event_tx: &EventSender) {
    match event.action() {
        Some(action) if action == "add" => {
            handle_device_added(device_name, event_tx);
        }

        Some(action) if action == "remove" => {
            debug!(device = device_name, "backlight device removed (udev)");
            let _ = event_tx.send(BrightnessEvent::DeviceRemoved(device_name.to_owned()));
        }

        _ => {}
    }
}

fn build_monitor_socket(subsystem: &str) -> Option<udev::MonitorSocket> {
    let monitor = udev::MonitorBuilder::new()
        .and_then(|builder| builder.match_subsystem(subsystem))
        .and_then(udev::MonitorBuilder::listen);

    match monitor {
        Ok(socket) => Some(socket),

        Err(err) => {
            warn!(
                error = %err,
                subsystem,
                "cannot create udev monitor"
            );
            None
        }
    }
}

fn handle_device_added(device_name: &str, event_tx: &EventSender) {
    let device_info = match sysfs::read_device(device_name) {
        Ok(info) => info,

        Err(err) => {
            warn!(
                device = device_name,
                error = %err,
                "cannot read newly added backlight device"
            );
            return;
        }
    };

    info!(device = device_name, "backlight device added (udev)");
    let _ = event_tx.send(BrightnessEvent::DeviceAdded(device_info));
}
