use wayle_core::NULL_PATH;
use zbus::{Connection, zvariant::OwnedObjectPath};

use super::{
    error::Error,
    proxy::{devices::DeviceProxy, manager::NetworkManagerProxy},
    types::device::NMDeviceType,
};

pub(crate) struct NetworkServiceDiscovery;

impl NetworkServiceDiscovery {
    pub async fn wifi_device_path(
        connection: &Connection,
    ) -> Result<Option<OwnedObjectPath>, Error> {
        Self::find_device_path(connection, NMDeviceType::Wifi).await
    }

    pub async fn wired_device_path(
        connection: &Connection,
    ) -> Result<Option<OwnedObjectPath>, Error> {
        Self::find_device_path(connection, NMDeviceType::Ethernet).await
    }

    async fn find_device_path(
        connection: &Connection,
        target_type: NMDeviceType,
    ) -> Result<Option<OwnedObjectPath>, Error> {
        let nm_proxy = NetworkManagerProxy::new(connection).await?;
        let devices = nm_proxy.get_all_devices().await.map_err(Error::DbusError)?;

        let mut candidates = Vec::new();

        for path in devices {
            let device_proxy = DeviceProxy::new(connection, path.clone())
                .await
                .map_err(Error::DbusError)?;

            let device_type = device_proxy.device_type().await.map_err(Error::DbusError)?;

            if device_type != target_type as u32 {
                continue;
            }

            let active = device_proxy
                .active_connection()
                .await
                .map_err(Error::DbusError)?;

            candidates.push((path, active.as_str() != NULL_PATH));
        }

        Ok(select_device(candidates))
    }
}

/// Picks the device the module should bind to, preferring one that is carrying
/// a connection.
///
/// A laptop in a dock has two ethernet devices; taking the first one
/// `GetAllDevices` happens to list binds the module to whichever NM enumerated
/// first, which is regularly the builtin NIC sitting down while the dock NIC
/// carries the traffic. Falling back to the first candidate keeps a
/// single-device machine — and a machine that is simply offline — working as
/// before.
fn select_device(candidates: Vec<(OwnedObjectPath, bool)>) -> Option<OwnedObjectPath> {
    let active = candidates.iter().position(|(_, is_active)| *is_active);

    match active {
        Some(index) => candidates.into_iter().nth(index).map(|(path, _)| path),
        None => candidates.into_iter().next().map(|(path, _)| path),
    }
}

#[cfg(test)]
mod tests {
    use zbus::zvariant::ObjectPath;

    use super::*;

    fn device(raw: &str, is_active: bool) -> (OwnedObjectPath, bool) {
        (ObjectPath::try_from(raw).unwrap().into(), is_active)
    }

    #[test]
    fn select_device_prefers_the_active_device_over_an_earlier_idle_one() {
        let candidates = vec![
            device("/org/freedesktop/NetworkManager/Devices/1", false),
            device("/org/freedesktop/NetworkManager/Devices/59", true),
        ];

        let selected = select_device(candidates);

        assert_eq!(
            selected.as_deref().map(ObjectPath::as_str),
            Some("/org/freedesktop/NetworkManager/Devices/59")
        );
    }

    #[test]
    fn select_device_falls_back_to_the_first_device_when_none_is_active() {
        let candidates = vec![
            device("/org/freedesktop/NetworkManager/Devices/1", false),
            device("/org/freedesktop/NetworkManager/Devices/59", false),
        ];

        let selected = select_device(candidates);

        assert_eq!(
            selected.as_deref().map(ObjectPath::as_str),
            Some("/org/freedesktop/NetworkManager/Devices/1")
        );
    }

    #[test]
    fn select_device_returns_nothing_when_there_are_no_candidates() {
        assert_eq!(select_device(Vec::new()), None);
    }
}
