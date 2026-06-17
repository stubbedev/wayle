use std::{sync::Arc, time::Duration};

use tokio::sync::{RwLock, broadcast};
use tokio_util::sync::CancellationToken;
use tracing::{debug, info, warn};
use zbus::{Connection, fdo::DBusProxy, names::OwnedBusName};

use super::register_item;
use crate::{events::TrayEvent, proxy::status_notifier_item::StatusNotifierItemProxy};

const PROBE_TIMEOUT: Duration = Duration::from_millis(500);

/// Delays before each orphan-scan pass. Becoming the `StatusNotifierWatcher`
/// races against tray apps noticing the new watcher and answering an SNI probe;
/// a single pass at startup misses any app slow to respond (e.g. Slack),
/// stranding its icon until the app itself restarts. Re-scanning over a
/// spread-out schedule catches late responders. The first entry is zero, so the
/// initial pass still runs immediately.
const SCAN_SCHEDULE: [Duration; 5] = [
    Duration::from_millis(0),
    Duration::from_secs(1),
    Duration::from_secs(3),
    Duration::from_secs(8),
    Duration::from_secs(20),
];

/// Scans the bus for SNI items that didn't re-register after a watcher restart.
pub(crate) fn spawn_orphan_scan(
    connection: Connection,
    registered_items: Arc<RwLock<Vec<String>>>,
    event_tx: broadcast::Sender<TrayEvent>,
    cancellation_token: CancellationToken,
    own_name: String,
) {
    tokio::spawn(scan_bus(
        connection,
        registered_items,
        event_tx,
        cancellation_token,
        own_name,
    ));
}

async fn scan_bus(
    connection: Connection,
    registered_items: Arc<RwLock<Vec<String>>>,
    event_tx: broadcast::Sender<TrayEvent>,
    cancellation_token: CancellationToken,
    own_name: String,
) {
    let mut total = 0u32;

    for delay in SCAN_SCHEDULE {
        if !delay.is_zero() {
            tokio::select! {
                () = cancellation_token.cancelled() => return,
                () = tokio::time::sleep(delay) => {}
            }
        }
        if cancellation_token.is_cancelled() {
            return;
        }

        total += scan_once(
            &connection,
            &registered_items,
            &event_tx,
            &cancellation_token,
            &own_name,
        )
        .await;
    }

    if total > 0 {
        info!(count = total, "recovered orphaned SNI items");
    }
}

/// Runs a single bus sweep, registering any orphaned SNI item it can probe.
/// Returns how many it recovered this pass.
#[allow(clippy::cognitive_complexity)]
async fn scan_once(
    connection: &Connection,
    registered_items: &Arc<RwLock<Vec<String>>>,
    event_tx: &broadcast::Sender<TrayEvent>,
    cancellation_token: &CancellationToken,
    own_name: &str,
) -> u32 {
    let Some(candidates) = list_candidate_names(connection, own_name).await else {
        return 0;
    };

    debug!(
        count = candidates.len(),
        "scanning bus for orphaned SNI items"
    );

    let mut found = 0u32;

    for bus_name in &candidates {
        if cancellation_token.is_cancelled() {
            return found;
        }

        let name_str = bus_name.as_str();

        if is_registered(registered_items, name_str).await {
            continue;
        }

        if !probe_sni(connection, name_str).await {
            continue;
        }

        if register_item(name_str, registered_items, event_tx, connection).await {
            info!(service = %name_str, "recovered orphaned SNI item");
            found += 1;
        }
    }

    found
}

async fn list_candidate_names(
    connection: &Connection,
    own_name: &str,
) -> Option<Vec<OwnedBusName>> {
    let dbus_proxy = match DBusProxy::new(connection).await {
        Ok(proxy) => proxy,
        Err(error) => {
            warn!(error = %error, "cannot create DBus proxy for orphan scan");
            return None;
        }
    };

    let bus_names = match dbus_proxy.list_names().await {
        Ok(names) => names,
        Err(error) => {
            warn!(error = %error, "cannot list bus names for orphan scan");
            return None;
        }
    };

    let candidates = bus_names
        .into_iter()
        .filter(|name| {
            let name = name.as_str();
            name.starts_with(':') && name != own_name
        })
        .collect();

    Some(candidates)
}

async fn is_registered(items: &Arc<RwLock<Vec<String>>>, bus_name: &str) -> bool {
    let items = items.read().await;
    let prefix = format!("{bus_name}/");

    items
        .iter()
        .any(|registered| registered == bus_name || registered.starts_with(&prefix))
}

async fn probe_sni(connection: &Connection, bus_name: &str) -> bool {
    let probe = async {
        let proxy = StatusNotifierItemProxy::builder(connection)
            .destination(bus_name)?
            .build()
            .await?;
        proxy.id().await
    };

    matches!(tokio::time::timeout(PROBE_TIMEOUT, probe).await, Ok(Ok(_)))
}
