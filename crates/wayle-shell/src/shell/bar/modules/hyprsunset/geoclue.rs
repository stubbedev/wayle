//! One-shot location lookup via GeoClue2 (system D-Bus), so the auto-schedule
//! can find sunrise/sunset without hand-entered coordinates.
//!
//! Best-effort: any failure (no daemon, no agent, denied, timeout) returns
//! `None` and the caller falls back to the configured latitude/longitude.

use std::time::Duration;

use futures::StreamExt;
use tracing::debug;
use zbus::{Connection, Result, proxy, zvariant::OwnedObjectPath};

/// GeoClue accuracy level "city" — plenty for a sunrise/sunset schedule, and
/// the least privacy-invasive level that still yields coordinates.
const ACCURACY_CITY: u32 = 4;

/// How long to wait for the first location fix before giving up.
const FIX_TIMEOUT: Duration = Duration::from_secs(15);

#[proxy(
    interface = "org.freedesktop.GeoClue2.Manager",
    default_service = "org.freedesktop.GeoClue2",
    default_path = "/org/freedesktop/GeoClue2/Manager"
)]
trait Manager {
    async fn get_client(&self) -> Result<OwnedObjectPath>;
}

#[proxy(
    interface = "org.freedesktop.GeoClue2.Client",
    default_service = "org.freedesktop.GeoClue2"
)]
trait Client {
    async fn start(&self) -> Result<()>;
    async fn stop(&self) -> Result<()>;

    #[zbus(property)]
    fn set_desktop_id(&self, id: &str) -> Result<()>;

    #[zbus(property)]
    fn set_requested_accuracy_level(&self, level: u32) -> Result<()>;

    #[zbus(signal)]
    fn location_updated(&self, old: OwnedObjectPath, new: OwnedObjectPath) -> Result<()>;
}

#[proxy(
    interface = "org.freedesktop.GeoClue2.Location",
    default_service = "org.freedesktop.GeoClue2"
)]
trait Location {
    #[zbus(property)]
    fn latitude(&self) -> Result<f64>;

    #[zbus(property)]
    fn longitude(&self) -> Result<f64>;
}

/// Resolve the current `(latitude, longitude)` via GeoClue2, or `None`.
pub(super) async fn query_location() -> Option<(f64, f64)> {
    match try_query().await {
        Ok(coords) => Some(coords),
        Err(error) => {
            debug!(%error, "geoclue location lookup failed; falling back to configured coords");
            None
        }
    }
}

async fn try_query() -> Result<(f64, f64)> {
    let connection = Connection::system().await?;

    let manager = ManagerProxy::new(&connection).await?;
    let client_path = manager.get_client().await?;

    let client = ClientProxy::builder(&connection)
        .path(client_path)?
        .build()
        .await?;

    // DesktopId is mandatory before Start(); accuracy must be requested or the
    // daemon yields nothing.
    client.set_desktop_id("wayle").await?;
    client.set_requested_accuracy_level(ACCURACY_CITY).await?;

    let mut updates = client.receive_location_updated().await?;
    client.start().await?;

    let location_path = match tokio::time::timeout(FIX_TIMEOUT, updates.next()).await {
        Ok(Some(signal)) => signal.args()?.new().clone(),
        Ok(None) => {
            let _ = client.stop().await;
            return Err(zbus::Error::Failure("geoclue signal stream ended".into()));
        }
        Err(_) => {
            let _ = client.stop().await;
            return Err(zbus::Error::Failure(
                "geoclue location fix timed out".into(),
            ));
        }
    };

    let location = LocationProxy::builder(&connection)
        .path(location_path)?
        .build()
        .await?;

    let coords = (location.latitude().await?, location.longitude().await?);

    let _ = client.stop().await;
    Ok(coords)
}
