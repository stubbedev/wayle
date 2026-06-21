//! DDC/CI backend for external monitors over I²C.
//!
//! Unlike internal panels (sysfs / logind), external displays are driven
//! through the VESA DDC/CI protocol on the monitor's I²C bus. This requires
//! the `i2c-dev` kernel module and read/write access to `/dev/i2c-*` (usually
//! membership in the `i2c` group).
//!
//! All DDC I/O is **blocking and slow** (tens of milliseconds per call, and
//! enumeration can take seconds). Every public method here must therefore be
//! invoked from a blocking context (`tokio::task::spawn_blocking`).

use std::{
    collections::{HashMap, HashSet},
    sync::Mutex,
    thread,
    time::Duration,
};

use ddc_hi::{Ddc, Display};
use tracing::{debug, warn};

use crate::{
    Error,
    types::{BacklightInfo, BacklightType, DeviceName},
};

/// VCP feature code for monitor luminance (brightness).
const VCP_LUMINANCE: u8 = 0x10;

/// DDC/CI is unreliable: a transaction can be corrupted or NAK'd and needs a
/// retry. Total attempts per read/write, with a short pause between.
const DDC_ATTEMPTS: u32 = 3;
const DDC_RETRY_DELAY: Duration = Duration::from_millis(40);

/// Prefix for synthesized DDC device names. Includes `ddc` and `i2c` so the
/// shell's `friendly_device_name` resolves these to the "external" label.
const DDC_NAME_PREFIX: &str = "ddci2c-";

struct DdcDisplay {
    display: Display,
    /// VCP-reported maximum luminance, cached so brightness changes can be
    /// reported without a second (slow) DDC read.
    max: u32,
}

/// Owns the open DDC/CI display handles and serializes access to them.
///
/// Handles are stateful (they hold open I²C file descriptors) and DDC writes
/// to the same bus must not overlap, so all access goes through the mutex.
pub(crate) struct DdcManager {
    displays: Mutex<HashMap<String, DdcDisplay>>,
}

impl DdcManager {
    pub(crate) fn empty() -> Self {
        Self {
            displays: Mutex::new(HashMap::new()),
        }
    }

    /// Re-scans the I²C buses and reconciles the managed handles against what
    /// is currently connected, returning `(added, removed)` for the backend to
    /// translate into device events.
    ///
    /// **Blocking and slow** — run inside `spawn_blocking`. Handles for still-
    /// present monitors are replaced with fresh ones; vanished monitors are
    /// dropped (closing their I²C fds).
    pub(crate) fn refresh(&self) -> (Vec<BacklightInfo>, Vec<String>) {
        let scanned = scan();

        let Ok(mut map) = self.displays.lock() else {
            return (Vec::new(), Vec::new());
        };

        let present: HashSet<String> =
            scanned.iter().map(|(info, _)| info.name.to_string()).collect();
        let previous: HashSet<String> = map.keys().cloned().collect();

        let removed: Vec<String> = previous.difference(&present).cloned().collect();

        let mut added = Vec::new();
        let mut next = HashMap::with_capacity(scanned.len());
        for (info, display) in scanned {
            let name = info.name.to_string();
            if !previous.contains(&name) {
                added.push(info.clone());
            }
            next.insert(name, DdcDisplay { display, max: info.max_brightness });
        }

        *map = next;

        if !added.is_empty() || !removed.is_empty() {
            debug!(added = added.len(), removed = removed.len(), "DDC monitors reconciled");
        }

        (added, removed)
    }

    /// True if `name` refers to a managed DDC monitor.
    pub(crate) fn contains(&self, name: &str) -> bool {
        self.displays
            .lock()
            .is_ok_and(|map| map.contains_key(name))
    }

    /// Writes raw luminance to a monitor via DDC/CI.
    ///
    /// **Blocking and slow** — run inside `spawn_blocking`. Returns the cached
    /// VCP maximum on success so the caller can report the resulting state.
    pub(crate) fn set_brightness(&self, name: &str, value: u32) -> Result<u32, Error> {
        let mut map = self
            .displays
            .lock()
            .map_err(|_| Error::DdcUnavailable { device: name.to_owned() })?;

        let display = map
            .get_mut(name)
            .ok_or_else(|| Error::DdcUnavailable { device: name.to_owned() })?;

        let clamped = value.min(display.max);
        let raw = u16::try_from(clamped).unwrap_or(u16::MAX);
        let max = display.max;

        with_retry(|| display.display.handle.set_vcp_feature(VCP_LUMINANCE, raw)).map_err(
            |detail| Error::Ddc {
                device: name.to_owned(),
                detail,
            },
        )?;

        Ok(max)
    }
}

/// Runs a DDC/CI transaction up to [`DDC_ATTEMPTS`] times, pausing between
/// tries. Returns the last error's text on exhaustion. **Blocking.**
fn with_retry<T, E: std::fmt::Display>(
    mut transaction: impl FnMut() -> Result<T, E>,
) -> Result<T, String> {
    let mut last = String::new();

    for attempt in 1..=DDC_ATTEMPTS {
        match transaction() {
            Ok(value) => return Ok(value),
            Err(err) => {
                last = err.to_string();
                if attempt < DDC_ATTEMPTS {
                    thread::sleep(DDC_RETRY_DELAY);
                }
            }
        }
    }

    Err(last)
}

/// Probes every DDC/CI display and returns the readable ones paired with a
/// freshly built [`BacklightInfo`]. Unreadable monitors (no DDC support, bad
/// permissions, missing `i2c-dev`) are skipped with a warning.
///
/// **Blocking and slow** — only call from a blocking context.
fn scan() -> Vec<(BacklightInfo, Display)> {
    let displays = Display::enumerate();
    debug!(count = displays.len(), "DDC displays returned by enumeration");

    displays
        .into_iter()
        .filter_map(|mut display| {
            let name = device_name(&display);
            match read_luminance(&mut display) {
                Ok((current, max)) => {
                    debug!(device = %name, current, max, "external monitor found");
                    let info = BacklightInfo {
                        name: DeviceName::new(name),
                        backlight_type: BacklightType::Ddc,
                        brightness: current,
                        max_brightness: max,
                    };
                    Some((info, display))
                }
                Err(detail) => {
                    warn!(device = %name, %detail, "skipping monitor: DDC luminance unreadable");
                    None
                }
            }
        })
        .collect()
}

/// Reads `(current, maximum)` luminance from a display. Returns the error as a
/// string because the DDC handle's error type is not `Clone`/`'static`-bound.
fn read_luminance(display: &mut Display) -> Result<(u32, u32), String> {
    let value = with_retry(|| display.handle.get_vcp_feature(VCP_LUMINANCE))?;
    Ok((u32::from(value.value()), u32::from(value.maximum())))
}

/// Builds a stable, recognizable device name from the display's backend id,
/// falling back to the model name. The result is sanitized to the same shape
/// as sysfs names (alphanumeric plus `-`).
fn device_name(display: &Display) -> String {
    let raw = if display.info.id.is_empty() {
        display
            .info
            .model_name
            .clone()
            .unwrap_or_else(|| String::from("unknown"))
    } else {
        display.info.id.clone()
    };

    format!("{DDC_NAME_PREFIX}{}", sanitize(&raw))
}

fn sanitize(raw: &str) -> String {
    raw.chars()
        .map(|c| if c.is_ascii_alphanumeric() { c } else { '-' })
        .collect()
}
