use std::{collections::HashMap, sync::Arc, time::Instant};

use tokio::sync::broadcast;
use tokio_util::sync::CancellationToken;
use tracing::{debug, info};
use wayle_core::Property;
use wayle_traits::ServiceMonitoring;
use zbus::Connection;

use crate::{
    dbus::{self, SERVICE_NAME},
    error::Error,
    service::WallpaperService,
    tasks::{spawn_color_extractor, spawn_output_watcher},
    types::ColorExtractorConfig,
};

/// Builder for configuring a WallpaperService.
#[derive(Debug)]
#[derive(Default)]
pub struct WallpaperServiceBuilder {
    color_extractor: ColorExtractorConfig,
    theming_monitor: Option<String>,
    shared_cycle: bool,
}


impl WallpaperServiceBuilder {
    /// Creates a new builder with default values.
    pub fn new() -> Self {
        Self::default()
    }

    /// Builds and initializes the WallpaperService.
    ///
    /// # Errors
    ///
    /// Returns error if D-Bus connection fails or service registration fails.
    #[allow(clippy::cognitive_complexity)]
    pub async fn build(self) -> Result<Arc<WallpaperService>, Error> {
        let start = Instant::now();

        let connection = Self::connect_session_bus().await?;
        debug!(elapsed_ms = start.elapsed().as_millis(), "D-Bus connected");

        let service = self.create_service(&connection);
        dbus::register(&connection, &service).await?;
        debug!(elapsed_ms = start.elapsed().as_millis(), "D-Bus registered");

        Self::start_background_tasks(&service).await?;
        debug!(
            elapsed_ms = start.elapsed().as_millis(),
            "Monitoring started"
        );

        info!("Wallpaper service registered at {SERVICE_NAME}");

        Ok(service)
    }

    /// Sets the color extraction configuration.
    pub fn color_extractor(mut self, config: ColorExtractorConfig) -> Self {
        self.color_extractor = config;
        self
    }

    /// Sets which monitor's wallpaper drives color extraction.
    pub fn theming_monitor(mut self, monitor: Option<String>) -> Self {
        self.theming_monitor = monitor;
        self
    }

    /// Synchronizes cycling across all monitors in shuffle mode.
    pub fn shared_cycle(mut self, shared: bool) -> Self {
        self.shared_cycle = shared;
        self
    }

    async fn connect_session_bus() -> Result<Connection, Error> {
        Connection::session().await.map_err(|error| {
            Error::ServiceInitializationFailed(format!("D-Bus connection failed: {error}"))
        })
    }

    fn create_service(self, connection: &Connection) -> Arc<WallpaperService> {
        let cancellation_token = CancellationToken::new();
        let (extraction_complete, _) = broadcast::channel(16);

        Arc::new(WallpaperService {
            cancellation_token,
            _connection: connection.clone(),
            last_extracted_wallpaper: Property::new(None),
            extraction_complete,
            theming_monitor: Property::new(self.theming_monitor),
            cycling: Property::new(None),
            monitors: Property::new(HashMap::new()),
            color_extractor: Property::new(self.color_extractor),
            shared_cycle: Property::new(self.shared_cycle),
        })
    }

    async fn start_background_tasks(service: &Arc<WallpaperService>) -> Result<(), Error> {
        service.start_monitoring().await?;
        spawn_output_watcher(Arc::clone(service));
        spawn_color_extractor(Arc::clone(service));
        Ok(())
    }
}
