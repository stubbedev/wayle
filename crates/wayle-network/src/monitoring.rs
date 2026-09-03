use std::{sync::Arc, time::Duration};

use futures::Stream;
use tokio_stream::StreamExt;
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};
use wayle_core::Property;
use wayle_traits::{Reactive, ServiceMonitoring};
use zbus::{Connection, proxy::CacheProperties, zvariant::OwnedObjectPath};

use super::{
    core::settings::Settings,
    discovery::NetworkServiceDiscovery,
    error::Error,
    proxy::manager::NetworkManagerProxy,
    service::NetworkService,
    types::connectivity::ConnectionType,
    wifi::{LiveWifiParams, Wifi},
    wired::{LiveWiredParams, Wired},
};

/// How often the monitors re-read NetworkManager instead of trusting the
/// property streams.
///
/// The streams are the fast path and carry every real change; this is the
/// safety net for the case where one of them stops delivering (observed after
/// a day of uptime and several suspend cycles), which otherwise pins the
/// module to whatever it last saw for the life of the process.
const RECONCILE_INTERVAL: Duration = Duration::from_secs(60);

impl ServiceMonitoring for NetworkService {
    type Error = Error;

    async fn start_monitoring(&self) -> Result<(), Self::Error> {
        spawn_primary_monitoring(
            self.zbus_connection.clone(),
            self.primary.clone(),
            self.cancellation_token.child_token(),
        )
        .await?;

        spawn_device_monitoring(
            self.zbus_connection.clone(),
            self.wifi.clone(),
            self.wired.clone(),
            self.settings.clone(),
            self.cancellation_token.child_token(),
        )
        .await
    }
}

async fn spawn_primary_monitoring(
    connection: Connection,
    primary: Property<ConnectionType>,
    cancellation_token: CancellationToken,
) -> Result<(), Error> {
    let nm_proxy = NetworkManagerProxy::new(&connection)
        .await
        .map_err(Error::DbusError)?;

    let initial_type = nm_proxy.primary_connection_type().await?;
    update_primary_connection(&initial_type, &primary);

    let mut type_changed = Some(nm_proxy.receive_primary_connection_type_changed().await);

    tokio::spawn(async move {
        let mut reconcile = reconcile_interval();

        loop {
            tokio::select! {
                _ = cancellation_token.cancelled() => {
                    debug!("NetworkMonitoring primary monitoring cancelled");
                    return;
                }
                change = next_or_pending(&mut type_changed) => {
                    match change {
                        Some(change) => {
                            if let Ok(nm_type) = change.get().await {
                                debug!(nm_type = %nm_type, "Primary connection type changed");
                                update_primary_connection(&nm_type, &primary);
                            }
                        }
                        None => {
                            warn!(
                                "NetworkManager PrimaryConnectionType stream ended; \
                                 primary connection now follows the periodic reconcile only"
                            );
                            type_changed = None;
                        }
                    }
                }
                _ = reconcile.tick() => {
                    resync_primary(&connection, &primary).await;
                }
            }
        }
    });

    Ok(())
}

async fn spawn_device_monitoring(
    connection: Connection,
    wifi: Property<Option<Arc<Wifi>>>,
    wired: Property<Option<Arc<Wired>>>,
    settings: Arc<Settings>,
    cancellation_token: CancellationToken,
) -> Result<(), Error> {
    let nm_proxy = NetworkManagerProxy::new(&connection)
        .await
        .map_err(Error::DbusError)?;

    let mut device_added = Some(nm_proxy.receive_device_added().await?);
    let mut device_removed = Some(nm_proxy.receive_device_removed().await?);

    tokio::spawn(async move {
        let mut reconcile = reconcile_interval();

        loop {
            tokio::select! {
                _ = cancellation_token.cancelled() => {
                    debug!("NetworkMonitoring device monitoring cancelled");
                    return;
                }
                signal = next_or_pending(&mut device_added) => {
                    match signal {
                        Some(signal) => {
                            if let Ok(args) = signal.args() {
                                debug!(path = %args.device_path, "Network device added");
                            }
                        }
                        None => {
                            warn!(
                                "NetworkManager DeviceAdded stream ended; \
                                 hot-plug now follows the periodic reconcile only"
                            );
                            device_added = None;
                            continue;
                        }
                    }

                    sync_devices(&connection, &wifi, &wired, &settings, &cancellation_token).await;
                }
                signal = next_or_pending(&mut device_removed) => {
                    match signal {
                        Some(signal) => {
                            if let Ok(args) = signal.args() {
                                debug!(path = %args.device_path, "Network device removed");
                            }
                        }
                        None => {
                            warn!(
                                "NetworkManager DeviceRemoved stream ended; \
                                 hot-unplug now follows the periodic reconcile only"
                            );
                            device_removed = None;
                            continue;
                        }
                    }

                    sync_devices(&connection, &wifi, &wired, &settings, &cancellation_token).await;
                }
                _ = reconcile.tick() => {
                    sync_devices(&connection, &wifi, &wired, &settings, &cancellation_token).await;
                }
            }
        }
    });

    Ok(())
}

/// A reconcile ticker whose immediate first tick has been consumed.
///
/// [`tokio::time::interval`] fires straight away, which would re-read
/// NetworkManager the instant monitoring starts — the caller has just done
/// that.
fn reconcile_interval() -> tokio::time::Interval {
    let mut interval = tokio::time::interval(RECONCILE_INTERVAL);
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    interval.reset();
    interval
}

/// Awaits the next item of a stream that may already have ended.
///
/// Written as a `select!` arm, `Some(item) = stream.next()` disables its own
/// branch the moment the stream ends: the pattern stops matching, `select!`
/// drops the arm, and the task parks on whatever is left with no log and no
/// exit. Yielding the `None` once lets the caller say so out loud and then
/// park only that arm, by leaving the option empty.
async fn next_or_pending<S>(stream: &mut Option<S>) -> Option<S::Item>
where
    S: Stream + Unpin,
{
    match stream {
        Some(stream) => stream.next().await,
        None => std::future::pending().await,
    }
}

/// Re-reads `PrimaryConnectionType` straight from NetworkManager.
///
/// Deliberately builds an uncached proxy: the long-lived proxy's property
/// cache is fed by the very `PropertiesChanged` delivery this is meant to
/// recover from, so reading through it would just return the stale value
/// again.
async fn resync_primary(connection: &Connection, primary: &Property<ConnectionType>) {
    let nm_proxy = NetworkManagerProxy::builder(connection)
        .cache_properties(CacheProperties::No)
        .build()
        .await;

    let nm_type = match nm_proxy {
        Ok(proxy) => proxy.primary_connection_type().await,
        Err(err) => {
            warn!(error = %err, "cannot reach NetworkManager to reconcile primary connection");
            return;
        }
    };

    match nm_type {
        Ok(nm_type) => update_primary_connection(&nm_type, primary),
        Err(err) => {
            warn!(error = %err, "cannot re-read NetworkManager primary connection type");
        }
    }
}

// ponytail: a rebind leaves the previous device's monitoring tasks running
// until the service shuts down, because they hold the service-level
// cancellation token. Rebinds happen on dock/undock, not in a loop, so the
// leak is bounded by how often the cable moves; give each device its own child
// token if that ever stops being true.
async fn sync_devices(
    connection: &Connection,
    wifi: &Property<Option<Arc<Wifi>>>,
    wired: &Property<Option<Arc<Wired>>>,
    settings: &Arc<Settings>,
    cancellation_token: &CancellationToken,
) {
    sync_wifi(connection, wifi, settings, cancellation_token).await;
    sync_wired(connection, wired, cancellation_token).await;
}

/// What a reconcile should do with a device property.
#[derive(Debug, Clone, PartialEq, Eq)]
enum DeviceSync {
    /// The bound device is still the one discovery picks: nothing to do.
    Keep,
    /// Discovery found no device of this type: drop what is bound.
    Clear,
    /// Discovery picked a different device: rebind to it.
    Rebind(OwnedObjectPath),
}

/// Decides how a device property should follow discovery.
///
/// Binding on "is it empty?" is what let a dock cycle strand the module: when
/// NM announced the replacement device before removing the old one, the add
/// was skipped as already-populated and the following remove cleared the
/// property. Comparing paths instead makes both orderings land on the live
/// device.
fn plan_device_sync(discovered: Option<&OwnedObjectPath>, bound: Option<&str>) -> DeviceSync {
    match (discovered, bound) {
        (None, None) => DeviceSync::Keep,
        (None, Some(_)) => DeviceSync::Clear,
        (Some(discovered), Some(bound)) if discovered.as_str() == bound => DeviceSync::Keep,
        (Some(discovered), _) => DeviceSync::Rebind(discovered.clone()),
    }
}

async fn sync_wifi(
    connection: &Connection,
    wifi: &Property<Option<Arc<Wifi>>>,
    settings: &Arc<Settings>,
    cancellation_token: &CancellationToken,
) {
    let discovered = match NetworkServiceDiscovery::wifi_device_path(connection).await {
        Ok(path) => path,
        Err(err) => {
            warn!(error = %err, "cannot discover WiFi device");
            return;
        }
    };

    let current = wifi.get();
    let bound = current
        .as_ref()
        .map(|wifi| wifi.device.core.object_path.as_str());

    match plan_device_sync(discovered.as_ref(), bound) {
        DeviceSync::Keep => {}
        DeviceSync::Clear => {
            debug!("WiFi device no longer present");
            wifi.set(None);
        }
        DeviceSync::Rebind(path) => {
            match Wifi::get_live(LiveWifiParams {
                connection,
                device_path: path.clone(),
                cancellation_token,
                settings: settings.clone(),
            })
            .await
            {
                Ok(new_wifi) => {
                    debug!(path = %path, "WiFi device initialized");
                    wifi.set(Some(new_wifi));
                }
                Err(err) => {
                    warn!(error = %err, path = %path, "Failed to initialize WiFi device");
                }
            }
        }
    }
}

async fn sync_wired(
    connection: &Connection,
    wired: &Property<Option<Arc<Wired>>>,
    cancellation_token: &CancellationToken,
) {
    let discovered = match NetworkServiceDiscovery::wired_device_path(connection).await {
        Ok(path) => path,
        Err(err) => {
            warn!(error = %err, "cannot discover wired device");
            return;
        }
    };

    let current = wired.get();
    let bound = current
        .as_ref()
        .map(|wired| wired.device.core.object_path.as_str());

    match plan_device_sync(discovered.as_ref(), bound) {
        DeviceSync::Keep => {}
        DeviceSync::Clear => {
            debug!("Wired device no longer present");
            wired.set(None);
        }
        DeviceSync::Rebind(path) => {
            match Wired::get_live(LiveWiredParams {
                connection,
                device_path: path.clone(),
                cancellation_token,
            })
            .await
            {
                Ok(new_wired) => {
                    debug!(path = %path, "Wired device initialized");
                    wired.set(Some(new_wired));
                }
                Err(err) => {
                    warn!(error = %err, path = %path, "Failed to initialize wired device");
                }
            }
        }
    }
}

fn update_primary_connection(nm_type: &str, primary: &Property<ConnectionType>) {
    let connection_type = ConnectionType::from_nm_type(nm_type);
    debug!(?connection_type, "Primary connection type resolved");
    primary.set(connection_type);
}

#[cfg(test)]
mod tests {
    use zbus::zvariant::ObjectPath;

    use super::*;

    fn path(raw: &str) -> OwnedObjectPath {
        ObjectPath::try_from(raw).unwrap().into()
    }

    #[test]
    fn plan_device_sync_keeps_the_bound_device_when_discovery_agrees() {
        let discovered = path("/org/freedesktop/NetworkManager/Devices/2");

        let plan = plan_device_sync(
            Some(&discovered),
            Some("/org/freedesktop/NetworkManager/Devices/2"),
        );

        assert_eq!(plan, DeviceSync::Keep);
    }

    #[test]
    fn plan_device_sync_rebinds_when_discovery_picks_another_device() {
        let discovered = path("/org/freedesktop/NetworkManager/Devices/59");

        let plan = plan_device_sync(
            Some(&discovered),
            Some("/org/freedesktop/NetworkManager/Devices/2"),
        );

        assert_eq!(plan, DeviceSync::Rebind(discovered));
    }

    #[test]
    fn plan_device_sync_binds_when_nothing_is_bound_yet() {
        let discovered = path("/org/freedesktop/NetworkManager/Devices/2");

        let plan = plan_device_sync(Some(&discovered), None);

        assert_eq!(plan, DeviceSync::Rebind(discovered));
    }

    #[test]
    fn plan_device_sync_clears_when_the_device_is_gone() {
        let plan = plan_device_sync(None, Some("/org/freedesktop/NetworkManager/Devices/2"));

        assert_eq!(plan, DeviceSync::Clear);
    }

    #[test]
    fn plan_device_sync_does_nothing_when_there_is_no_device_either_way() {
        assert_eq!(plan_device_sync(None, None), DeviceSync::Keep);
    }

    #[tokio::test]
    async fn next_or_pending_yields_stream_items_then_the_end_exactly_once() {
        let mut stream = Some(tokio_stream::iter([1, 2]));

        assert_eq!(next_or_pending(&mut stream).await, Some(1));
        assert_eq!(next_or_pending(&mut stream).await, Some(2));
        assert_eq!(next_or_pending(&mut stream).await, None);
    }

    #[tokio::test]
    async fn next_or_pending_parks_forever_once_the_stream_is_taken_away() {
        let mut stream: Option<tokio_stream::Iter<std::vec::IntoIter<u8>>> = None;

        let parked =
            tokio::time::timeout(Duration::from_millis(50), next_or_pending(&mut stream)).await;

        assert!(parked.is_err(), "an empty option must never resolve");
    }
}
