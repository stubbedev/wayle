use std::sync::{Arc, Mutex};

use tokio::sync::{broadcast, mpsc};
use tokio_util::sync::CancellationToken;
use tracing::info;
use wayle_core::Property;
use wayle_traits::ServiceMonitoring;

use crate::{backend, error::Error, service::BrightnessService};

const EVENT_CHANNEL_CAPACITY: usize = 100;

/// Configuration for [`BrightnessService`](crate::BrightnessService) construction.
pub struct BrightnessServiceBuilder {
    external_monitors: bool,
}

impl Default for BrightnessServiceBuilder {
    fn default() -> Self {
        Self {
            external_monitors: true,
        }
    }
}

impl BrightnessServiceBuilder {
    pub(crate) fn new() -> Self {
        Self::default()
    }

    /// Enables discovery and control of external monitors over DDC/CI (I²C).
    ///
    /// Enabled by default. Requires the `i2c-dev` kernel module and access to
    /// `/dev/i2c-*`. Disable to skip the (slow) DDC enumeration entirely.
    #[must_use]
    pub fn external_monitors(mut self, enabled: bool) -> Self {
        self.external_monitors = enabled;
        self
    }

    /// Returns `Ok(None)` only when there are no internal backlight devices
    /// *and* external monitor support is disabled (desktops, servers, VMs).
    /// When external support is on, the service starts so DDC monitors found
    /// during the backend's asynchronous enumeration can populate it.
    ///
    /// # Errors
    ///
    /// Returns error if backend initialization fails.
    pub async fn build(self) -> Result<Option<Arc<BrightnessService>>, Error> {
        let initial_devices = backend::sysfs::enumerate();

        if initial_devices.is_empty() && !self.external_monitors {
            info!("no backlight devices found, brightness service disabled");
            return Ok(None);
        }

        let device_count = initial_devices.len();

        let (command_tx, command_rx) = mpsc::unbounded_channel();
        let (event_tx, _) = broadcast::channel(EVENT_CHANNEL_CAPACITY);
        let cancellation_token = CancellationToken::new();

        let devices = Property::new(Vec::new());

        let service = Arc::new(BrightnessService {
            command_tx,
            event_tx,
            cancellation_token,
            backend_handle: Mutex::new(None),
            devices,
        });

        service.start_monitoring().await?;

        let backend_handle = backend::start(
            initial_devices,
            self.external_monitors,
            command_rx,
            service.event_tx.clone(),
            service.cancellation_token.child_token(),
        );

        service.set_backend_handle(backend_handle);

        info!(device_count, "brightness service started");

        Ok(Some(service))
    }
}
