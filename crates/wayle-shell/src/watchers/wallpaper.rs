//! Wallpaper service hot-reload watcher.

use std::{path::PathBuf, sync::Arc, time::Duration};

use futures::StreamExt;
use tracing::warn;
use wayle_config::schemas::wallpaper::{MonitorWallpaperConfig, WallpaperConfig};
use wayle_wallpaper::WallpaperService;

use crate::{shell::ShellServices, wallpaper_map};

pub(crate) fn spawn(services: &ShellServices) {
    let Some(wallpaper) = services.wallpaper.clone() else {
        return;
    };

    let config = services.config.config().wallpaper.clone();

    spawn_single_file_watcher(&config, &wallpaper);
    spawn_fit_mode_watcher(&config, &wallpaper);
    spawn_cycling_watcher(&config, &wallpaper);
    spawn_cycling_interval_watcher(&config, &wallpaper);
    spawn_shared_cycle_watcher(&config, &wallpaper);
    spawn_monitors_watcher(&config, &wallpaper);
}

/// Applies the global `fit-mode` to all monitors when it changes. The render
/// surfaces re-read the fit from config; setting it on the service is what
/// pokes the `monitors` property so a re-render happens.
fn spawn_fit_mode_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let fit_mode = config.fit_mode.clone();
    let wallpaper = wallpaper.clone();

    let mut stream = fit_mode.watch();

    tokio::spawn(async move {
        stream.next().await;

        while let Some(mode) = stream.next().await {
            if let Err(e) = wallpaper
                .set_fit_mode(wallpaper_map::fit_mode(mode), None)
                .await
            {
                warn!(error = %e, "cannot apply fit mode from config change");
            }
        }
    });
}

/// Applies the global single-file `wallpaper` to all monitors when it changes
/// (and cycling is not active).
fn spawn_single_file_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let wallpaper_path = config.wallpaper.clone();
    let cycling_enabled = config.cycling_enabled.clone();
    let wallpaper = wallpaper.clone();

    let mut stream = wallpaper_path.watch();

    tokio::spawn(async move {
        stream.next().await;

        while let Some(path) = stream.next().await {
            if path.is_empty() || cycling_enabled.get() {
                continue;
            }
            if let Err(e) = wallpaper.set_wallpaper(PathBuf::from(path), None).await {
                warn!(error = %e, "cannot apply single-file wallpaper from config change");
            }
        }
    });
}

fn spawn_cycling_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let cycling_enabled = config.cycling_enabled.clone();
    let cycling_directory = config.cycling_directory.clone();
    let cycling_mode = config.cycling_mode.clone();
    let cycling_interval = config.cycling_interval_mins.clone();
    let monitors_config = config.monitors.clone();
    let wallpaper = wallpaper.clone();

    let mut enabled_stream = cycling_enabled.watch();
    let mut directory_stream = cycling_directory.watch();
    let mut mode_stream = cycling_mode.watch();

    tokio::spawn(async move {
        enabled_stream.next().await;
        directory_stream.next().await;
        mode_stream.next().await;

        loop {
            tokio::select! {
                Some(_) = enabled_stream.next() => {}
                Some(_) = directory_stream.next() => {}
                Some(_) = mode_stream.next() => {}
                else => break,
            }

            if !cycling_enabled.get() {
                wallpaper.stop_cycling();
                restore_monitor_wallpapers(&wallpaper, &monitors_config.get()).await;
                continue;
            }

            let directory = cycling_directory.get();
            if directory.is_empty() {
                continue;
            }

            let mode = wallpaper_map::cycling_mode(cycling_mode.get());
            let interval = Duration::from_secs(cycling_interval.get().value() * 60);

            if let Err(e) = wallpaper.start_cycling(PathBuf::from(directory), interval, mode) {
                warn!(error = %e, "could not apply cycling config change");
            }
        }
    });
}

fn spawn_cycling_interval_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let mut stream = config.cycling_interval_mins.watch();
    let wallpaper = wallpaper.clone();

    tokio::spawn(async move {
        stream.next().await;

        while let Some(interval) = stream.next().await {
            wallpaper.set_cycling_interval(Duration::from_secs(interval.value() * 60));
        }
    });
}

fn spawn_shared_cycle_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let mut stream = config.cycling_same_image.watch();
    let wallpaper = wallpaper.clone();

    tokio::spawn(async move {
        stream.next().await;

        while let Some(shared) = stream.next().await {
            wallpaper.shared_cycle.set(shared);
        }
    });
}

fn spawn_monitors_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let mut stream = config.monitors.watch();
    let wallpaper = wallpaper.clone();

    tokio::spawn(async move {
        stream.next().await;

        while let Some(monitor_configs) = stream.next().await {
            for monitor_cfg in &monitor_configs {
                apply_monitor_config_change(&wallpaper, monitor_cfg).await;
            }
        }
    });
}

async fn apply_monitor_config_change(
    wallpaper: &WallpaperService,
    monitor_cfg: &MonitorWallpaperConfig,
) {
    if monitor_cfg.name.is_empty() {
        return;
    }

    let fit_mode = wallpaper_map::fit_mode(monitor_cfg.fit_mode);

    if let Err(e) = wallpaper
        .set_fit_mode(fit_mode, Some(&monitor_cfg.name))
        .await
    {
        warn!(
            error = %e,
            monitor = %monitor_cfg.name,
            "could not apply fit mode from config change"
        );
    }

    if monitor_cfg.wallpaper.is_empty() {
        return;
    }

    let path = PathBuf::from(&monitor_cfg.wallpaper);
    if let Err(e) = wallpaper.set_wallpaper(path, Some(&monitor_cfg.name)).await {
        warn!(
            error = %e,
            monitor = %monitor_cfg.name,
            "could not apply wallpaper from config change"
        );
    }
}

async fn restore_monitor_wallpapers(
    wallpaper: &WallpaperService,
    monitors: &[MonitorWallpaperConfig],
) {
    for monitor_cfg in monitors {
        if monitor_cfg.name.is_empty() || monitor_cfg.wallpaper.is_empty() {
            continue;
        }

        let path = PathBuf::from(&monitor_cfg.wallpaper);
        if let Err(e) = wallpaper.set_wallpaper(path, Some(&monitor_cfg.name)).await {
            warn!(
                error = %e,
                monitor = %monitor_cfg.name,
                "cannot restore monitor wallpaper"
            );
        }
    }
}
