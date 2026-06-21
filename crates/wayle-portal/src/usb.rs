//! `org.freedesktop.impl.portal.Usb`.
//!
//! Device *enumeration* lives in the portal frontend (udev); the backend only
//! grants access. `AcquireDevices` returns the requested devices' access
//! options as the granted set. Auto-grants for now (a future revision can route
//! to the Access dialog).

use std::collections::HashMap;

use crate::{dbus_util::owned, response::Response};
use zbus::{
    interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

/// A device as passed to `AcquireDevices`: `(id, info, access_options)`.
type RequestedDevice = (String, HashMap<String, OwnedValue>, HashMap<String, OwnedValue>);
/// A granted device: `(id, access_options)`.
type GrantedDevice = (String, HashMap<String, OwnedValue>);

/// Usb portal interface.
pub struct Usb;

impl Usb {
    /// Builds the interface.
    #[must_use]
    pub fn new() -> Self {
        Self
    }
}

impl Default for Usb {
    fn default() -> Self {
        Self::new()
    }
}

#[interface(name = "org.freedesktop.impl.portal.Usb")]
impl Usb {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Grants access to the requested USB devices.
    async fn acquire_devices(
        &self,
        _handle: OwnedObjectPath,
        _parent_window: String,
        _app_id: String,
        devices: Vec<RequestedDevice>,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let granted = grant(&devices);
        let results = owned(granted)
            .map(|v| HashMap::from([("devices".to_owned(), v)]))
            .unwrap_or_default();
        (Response::Success.code(), results)
    }
}

/// Projects requested devices to the granted `(id, access_options)` set.
fn grant(devices: &[RequestedDevice]) -> Vec<GrantedDevice> {
    devices
        .iter()
        .map(|(id, _info, access)| (id.clone(), clone_vardict(access)))
        .collect()
}

/// Deep-clones a vardict.
fn clone_vardict(map: &HashMap<String, OwnedValue>) -> HashMap<String, OwnedValue> {
    map.iter()
        .filter_map(|(k, v)| Some((k.clone(), v.try_clone().ok()?)))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use zbus::zvariant::Value;

    #[test]
    fn grant_keeps_id_and_access_drops_info() {
        let info = HashMap::from([("vendor".to_owned(), OwnedValue::try_from(Value::from("acme")).unwrap())]);
        let access =
            HashMap::from([("writable".to_owned(), OwnedValue::try_from(Value::from(true)).unwrap())]);
        let devices = vec![("dev0".to_owned(), info, access)];

        let granted = grant(&devices);
        assert_eq!(granted.len(), 1);
        assert_eq!(granted[0].0, "dev0");
        assert!(granted[0].1.contains_key("writable"));
    }
}
