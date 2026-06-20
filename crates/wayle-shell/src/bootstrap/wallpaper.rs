use std::{
    collections::HashMap,
    path::PathBuf,
    sync::Arc,
    time::{Duration, Instant},
};

use tracing::{debug, warn};
use wayle_config::schemas::wallpaper::MonitorWallpaperConfig;
use wayle_wallpaper::{ColorExtractorConfig, MonitorState, WallpaperService};

use crate::wallpaper_map;

pub(super) async fn build_wallpaper_service(
    cfg: &wayle_config::schemas::wallpaper::WallpaperConfig,
    theming_monitor: Option<String>,
    color_extractor: ColorExtractorConfig,
) -> Result<Arc<WallpaperService>, wayle_wallpaper::Error> {
    let t = Instant::now();
    let service = WallpaperService::builder()
        .theming_monitor(theming_monitor)
        .color_extractor(color_extractor)
        .shared_cycle(cfg.cycling_same_image.get())
        .build()
        .await?;
    debug!(elapsed_ms = t.elapsed().as_millis(), "Service built");

    // Resolution order: cycling (if enabled) → else the global single file.
    // Per-monitor overrides apply on top regardless. The shell renders natively
    // by watching the service's `monitors` state; no external tool is spawned.
    let cycling_started = start_cycling_from_config(&service, cfg);
    if !cycling_started {
        apply_single_file(&service, cfg).await;
    }

    apply_monitor_config(&service, cfg);
    debug!(
        elapsed_ms = t.elapsed().as_millis(),
        "Monitor config applied"
    );

    Ok(service)
}

/// Applies the global single image file to all monitors, if configured.
async fn apply_single_file(
    service: &Arc<WallpaperService>,
    cfg: &wayle_config::schemas::wallpaper::WallpaperConfig,
) {
    let path = cfg.wallpaper.get();
    if path.is_empty() {
        return;
    }
    if let Err(e) = service.set_wallpaper(PathBuf::from(path), None).await {
        warn!(error = %e, "cannot apply single-file wallpaper from config");
    }
}

fn start_cycling_from_config(
    service: &Arc<WallpaperService>,
    cfg: &wayle_config::schemas::wallpaper::WallpaperConfig,
) -> bool {
    // Cycling is on whenever a directory is configured.
    let directory = cfg.cycling_directory.get();
    if directory.is_empty() {
        return false;
    }

    let mode = wallpaper_map::cycling_mode(cfg.cycling_mode.get());
    let interval = Duration::from_secs(cfg.cycling_interval_mins.get().value() * 60);

    if let Err(e) = service.start_cycling(PathBuf::from(directory), interval, mode) {
        warn!(error = %e, "could not start wallpaper cycling from config");
        return false;
    }

    true
}

fn apply_monitor_config(
    service: &Arc<WallpaperService>,
    cfg: &wayle_config::schemas::wallpaper::WallpaperConfig,
) -> bool {
    let monitor_configs = cfg.monitors.get();
    if monitor_configs.is_empty() {
        return false;
    }

    let mut monitors = service.monitors.get();
    let mut has_wallpapers = false;

    for monitor_cfg in &monitor_configs {
        has_wallpapers |= apply_single_monitor(&mut monitors, monitor_cfg);
    }

    service.monitors.set(monitors);

    has_wallpapers
}

fn apply_single_monitor(
    monitors: &mut HashMap<String, MonitorState>,
    monitor_cfg: &MonitorWallpaperConfig,
) -> bool {
    if monitor_cfg.name.is_empty() {
        return false;
    }

    let Some(state) = monitors.get_mut(&monitor_cfg.name) else {
        return false;
    };

    state.fit_mode = wallpaper_map::fit_mode(monitor_cfg.fit_mode);

    if monitor_cfg.wallpaper.is_empty() {
        return false;
    }

    let path = PathBuf::from(&monitor_cfg.wallpaper);
    if !path.exists() {
        warn!(
            monitor = %monitor_cfg.name,
            path = %monitor_cfg.wallpaper,
            "wallpaper path not found"
        );
        return false;
    }

    state.wallpaper = Some(path);
    true
}
