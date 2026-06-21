//! DDC/CI backend for external monitors over I²C.
//!
//! Unlike internal panels (sysfs / logind), external displays are driven
//! through the VESA DDC/CI protocol on the monitor's I²C bus. This requires
//! the `i2c-dev` kernel module and read/write access to `/dev/i2c-*` (usually
//! membership in the `i2c` group).
//!
//! Built on the low-level `ddc-i2c` crate rather than `ddc-hi`: we only need
//! raw VCP luminance get/set, and `ddc-hi`'s EDID/MCCS capability layer drags
//! in unmaintained, future-incompatible dependencies.
//!
//! All DDC I/O is **blocking and slow** (tens of milliseconds per call, and a
//! full bus scan can take a while). Every public method here must therefore be
//! invoked from a blocking context (`tokio::task::spawn_blocking`).

use std::{
    collections::{HashMap, HashSet},
    fs,
    sync::Mutex,
    thread,
    time::Duration,
};

use ddc::Ddc;
use ddc_i2c::I2cDeviceDdc;
use tracing::{debug, warn};

use crate::{
    Error,
    types::{BacklightInfo, BacklightType, DeviceName},
};

/// VCP feature code for monitor luminance (brightness).
const VCP_LUMINANCE: u8 = 0x10;

/// DDC/CI is unreliable: a transaction can be corrupted or NAK'd and needs a
/// retry. Total attempts per write, with a short pause between.
const DDC_ATTEMPTS: u32 = 3;
const DDC_RETRY_DELAY: Duration = Duration::from_millis(40);

/// Where Linux exposes i2c buses, and the per-bus node prefix.
const DEV_DIR: &str = "/dev";
const I2C_NODE_PREFIX: &str = "i2c-";

/// Prefix for synthesized DDC device names. Includes `ddc` and `i2c` so the
/// shell's `friendly_device_name` resolves these to the "external" label.
const DDC_NAME_PREFIX: &str = "ddci2c-";

struct DdcDisplay {
    handle: I2cDeviceDdc,
    /// VCP-reported maximum luminance, cached so brightness changes can be
    /// reported without a second (slow) DDC read.
    max: u32,
}

/// Owns the open DDC/CI handles and serializes access to them.
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

        let present: HashSet<String> = scanned
            .iter()
            .map(|(info, _)| info.name.to_string())
            .collect();
        let previous: HashSet<String> = map.keys().cloned().collect();

        let removed: Vec<String> = previous.difference(&present).cloned().collect();

        let mut added = Vec::new();
        let mut next = HashMap::with_capacity(scanned.len());
        for (info, handle) in scanned {
            let name = info.name.to_string();
            if !previous.contains(&name) {
                added.push(info.clone());
            }
            next.insert(
                name,
                DdcDisplay {
                    handle,
                    max: info.max_brightness,
                },
            );
        }

        *map = next;

        if !added.is_empty() || !removed.is_empty() {
            debug!(
                added = added.len(),
                removed = removed.len(),
                "DDC monitors reconciled"
            );
        }

        (added, removed)
    }

    /// True if `name` refers to a managed DDC monitor.
    pub(crate) fn contains(&self, name: &str) -> bool {
        self.displays.lock().is_ok_and(|map| map.contains_key(name))
    }

    /// Writes raw luminance to a monitor via DDC/CI.
    ///
    /// **Blocking and slow** — run inside `spawn_blocking`. Returns the cached
    /// VCP maximum on success so the caller can report the resulting state.
    pub(crate) fn set_brightness(&self, name: &str, value: u32) -> Result<u32, Error> {
        let mut map = self.displays.lock().map_err(|_| Error::DdcUnavailable {
            device: name.to_owned(),
        })?;

        let display = map.get_mut(name).ok_or_else(|| Error::DdcUnavailable {
            device: name.to_owned(),
        })?;

        let clamped = value.min(display.max);
        let raw = u16::try_from(clamped).unwrap_or(u16::MAX);
        let max = display.max;

        with_retry(|| {
            display
                .handle
                .set_vcp_feature(VCP_LUMINANCE, raw)
                .map_err(|err| format!("{err:?}"))
        })
        .map_err(|detail| Error::Ddc {
            device: name.to_owned(),
            detail,
        })?;

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

/// Probes every `/dev/i2c-*` bus and returns the ones that answer the DDC/CI
/// luminance query, paired with a freshly built [`BacklightInfo`] and an open
/// handle. Buses with no DDC monitor (sensors, HDMI-audio, …) fail the probe
/// and are silently skipped; missing `i2c-dev` or permissions yields nothing.
///
/// The probe is single-attempt on purpose: most non-monitor buses never
/// answer, and retrying each would make the scan needlessly slow. Retries are
/// reserved for writes to a known monitor.
///
/// **Blocking and slow** — only call from a blocking context.
fn scan() -> Vec<(BacklightInfo, I2cDeviceDdc)> {
    let mut buses = i2c_buses();
    buses.sort_unstable();

    buses
        .into_iter()
        .filter_map(|bus| {
            let path = format!("{DEV_DIR}/{I2C_NODE_PREFIX}{bus}");
            let mut handle = match ddc_i2c::from_i2c_device(&path) {
                Ok(handle) => handle,
                Err(err) => {
                    debug!(%path, error = %err, "cannot open i2c bus");
                    return None;
                }
            };

            match handle.get_vcp_feature(VCP_LUMINANCE) {
                Ok(value) => {
                    let current = u32::from(value.value());
                    let max = u32::from(value.maximum());
                    let name = format!("{DDC_NAME_PREFIX}{bus}");
                    debug!(device = %name, current, max, "external monitor found");
                    let info = BacklightInfo {
                        name: DeviceName::new(name),
                        backlight_type: BacklightType::Ddc,
                        brightness: current,
                        max_brightness: max,
                    };
                    Some((info, handle))
                }
                Err(err) => {
                    debug!(%path, error = ?err, "no DDC monitor on bus");
                    None
                }
            }
        })
        .collect()
}

/// Bus numbers of every `/dev/i2c-N` node. Empty when `i2c-dev` is not loaded.
fn i2c_buses() -> Vec<u32> {
    let entries = match fs::read_dir(DEV_DIR) {
        Ok(entries) => entries,
        Err(err) => {
            warn!(error = %err, "cannot read {DEV_DIR} to find i2c buses");
            return Vec::new();
        }
    };

    entries
        .filter_map(Result::ok)
        .filter_map(|entry| {
            entry
                .file_name()
                .to_str()
                .and_then(|name| name.strip_prefix(I2C_NODE_PREFIX))
                .and_then(|n| n.parse::<u32>().ok())
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use std::cell::Cell;

    use super::*;

    #[test]
    fn retry_returns_first_success_without_retrying() {
        let calls = Cell::new(0);
        let result = with_retry(|| {
            calls.set(calls.get() + 1);
            Ok::<u32, String>(42)
        });

        assert_eq!(result, Ok(42));
        assert_eq!(calls.get(), 1, "should not retry after success");
    }

    #[test]
    fn retry_recovers_after_transient_failure() {
        let calls = Cell::new(0);
        let result = with_retry(|| {
            calls.set(calls.get() + 1);
            if calls.get() < 2 { Err("nak") } else { Ok(7) }
        });

        assert_eq!(result, Ok(7));
        assert_eq!(calls.get(), 2);
    }

    #[test]
    fn retry_exhausts_and_reports_last_error() {
        let calls = Cell::new(0);
        let result: Result<u32, String> = with_retry(|| {
            calls.set(calls.get() + 1);
            Err(format!("fail {}", calls.get()))
        });

        assert_eq!(result, Err(format!("fail {DDC_ATTEMPTS}")));
        assert_eq!(calls.get(), DDC_ATTEMPTS);
    }
}
