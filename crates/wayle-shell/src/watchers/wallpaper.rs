//! Wallpaper service hot-reload watcher.

use std::{collections::HashSet, path::PathBuf, sync::Arc, time::Duration};

use futures::StreamExt;
use tracing::warn;
use wayle_config::schemas::wallpaper::{
    FitMode as CfgFitMode, MonitorWallpaperConfig, WallpaperConfig,
};
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
    spawn_hotplug_watcher(&config, &wallpaper);
}

/// Applies config to monitors that show up after startup.
///
/// The service registers a hotplugged output with a blank state — it has no view
/// of config. Cycling seeds itself at registration, but a single-file or
/// per-monitor wallpaper does not: the monitor keeps `None` in the service
/// state, so color extraction and the D-Bus/CLI getters report nothing for it
/// even once the shell is rendering the right image.
fn spawn_hotplug_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let monitors_config = config.monitors.clone();
    let global_fit = config.fit_mode.clone();
    let single_file = config.wallpaper.clone();
    let wallpaper = wallpaper.clone();

    let mut stream = wallpaper.monitors.watch();

    tokio::spawn(async move {
        // Monitors present at build time already had config applied by
        // bootstrap; only ones appearing later need catching up.
        let mut known: HashSet<String> = wallpaper.monitors.get().keys().cloned().collect();

        while let Some(monitors) = stream.next().await {
            let fresh: Vec<String> = monitors
                .keys()
                .filter(|name| !known.contains(*name))
                .cloned()
                .collect();
            known = monitors.keys().cloned().collect();

            // Applying below re-emits `monitors`, but never with a new name, so
            // the next pass finds nothing fresh and this does not loop.
            for name in fresh {
                let entry = monitors_config.get().into_iter().find(|m| m.name == name);
                let cycling = wallpaper.cycling_config().is_some();

                let fit = hotplug_fit_mode(entry.as_ref(), global_fit.get());
                if let Err(e) = wallpaper
                    .set_fit_mode(wallpaper_map::fit_mode(fit), Some(&name))
                    .await
                {
                    warn!(error = %e, monitor = %name, "cannot apply fit mode to new monitor");
                }

                let Some(path) = hotplug_wallpaper(entry.as_ref(), &single_file.get(), cycling)
                else {
                    continue;
                };

                if let Err(e) = wallpaper
                    .set_wallpaper(PathBuf::from(path), Some(&name))
                    .await
                {
                    warn!(error = %e, monitor = %name, "cannot apply wallpaper to new monitor");
                }
            }
        }
    });
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
/// (and a cycling directory is not set, which would take precedence).
fn spawn_single_file_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let wallpaper_path = config.wallpaper.clone();
    let cycling_directory = config.cycling_directory.clone();
    let wallpaper = wallpaper.clone();

    let mut stream = wallpaper_path.watch();

    tokio::spawn(async move {
        stream.next().await;

        while let Some(path) = stream.next().await {
            if path.is_empty() || !cycling_directory.get().is_empty() {
                continue;
            }
            if let Err(e) = wallpaper.set_wallpaper(PathBuf::from(path), None).await {
                warn!(error = %e, "cannot apply single-file wallpaper from config change");
            }
        }
    });
}

/// Starts/stops cycling as the directory is set or cleared (a non-empty
/// directory means cycling is on).
fn spawn_cycling_watcher(config: &WallpaperConfig, wallpaper: &Arc<WallpaperService>) {
    let cycling_directory = config.cycling_directory.clone();
    let cycling_mode = config.cycling_mode.clone();
    let cycling_interval = config.cycling_interval_mins.clone();
    let single_file = config.wallpaper.clone();
    let monitors_config = config.monitors.clone();
    let wallpaper = wallpaper.clone();

    let mut directory_stream = cycling_directory.watch();
    let mut mode_stream = cycling_mode.watch();

    tokio::spawn(async move {
        directory_stream.next().await;
        mode_stream.next().await;

        loop {
            tokio::select! {
                Some(_) = directory_stream.next() => {}
                Some(_) = mode_stream.next() => {}
                else => break,
            }

            let directory = cycling_directory.get();
            if directory.is_empty() {
                // Cycling cleared — fall back to per-monitor / single-file.
                wallpaper.stop_cycling();
                restore_monitor_wallpapers(&wallpaper, &monitors_config.get()).await;
                let single = single_file.get();
                if !single.is_empty()
                    && let Err(e) = wallpaper.set_wallpaper(PathBuf::from(single), None).await
                {
                    warn!(error = %e, "cannot restore single-file wallpaper");
                }
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

/// The fit mode a monitor appearing after startup should be given: its own
/// `[[wallpaper.monitors]]` entry, else the global `fit-mode`.
fn hotplug_fit_mode(entry: Option<&MonitorWallpaperConfig>, global: CfgFitMode) -> CfgFitMode {
    entry.map_or(global, |m| m.fit_mode)
}

/// The wallpaper a monitor appearing after startup should be given.
///
/// `None` means leave the service state alone: cycling already seeded the
/// monitor at registration, and there is nothing to apply when neither a
/// per-monitor override nor a global image is configured.
fn hotplug_wallpaper(
    entry: Option<&MonitorWallpaperConfig>,
    single_file: &str,
    cycling_active: bool,
) -> Option<String> {
    if cycling_active {
        return None;
    }

    let path = entry
        .filter(|m| !m.wallpaper.is_empty())
        .map_or(single_file, |m| m.wallpaper.as_str());

    (!path.is_empty()).then(|| path.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn entry(wallpaper: &str, fit_mode: CfgFitMode) -> MonitorWallpaperConfig {
        MonitorWallpaperConfig {
            name: String::from("DP-1"),
            fit_mode,
            wallpaper: String::from(wallpaper),
        }
    }

    #[test]
    fn hotplug_uses_the_per_monitor_override_over_the_global_image() {
        let entry = entry("mine.png", CfgFitMode::Fill);

        assert_eq!(
            hotplug_wallpaper(Some(&entry), "global.png", false),
            Some(String::from("mine.png"))
        );
    }

    #[test]
    fn hotplug_falls_back_to_the_global_image() {
        assert_eq!(
            hotplug_wallpaper(None, "global.png", false),
            Some(String::from("global.png"))
        );

        // An entry that only sets a fit mode still takes the global image.
        let entry = entry("", CfgFitMode::Fit);
        assert_eq!(
            hotplug_wallpaper(Some(&entry), "global.png", false),
            Some(String::from("global.png"))
        );
    }

    #[test]
    fn hotplug_applies_nothing_while_cycling() {
        // Registration already seeded the monitor from the cycle; overwriting it
        // here would knock it back to the static image.
        let entry = entry("mine.png", CfgFitMode::Fill);

        assert_eq!(hotplug_wallpaper(Some(&entry), "global.png", true), None);
        assert_eq!(hotplug_wallpaper(None, "global.png", true), None);
    }

    #[test]
    fn hotplug_applies_nothing_with_no_image_configured() {
        assert_eq!(hotplug_wallpaper(None, "", false), None);
        assert_eq!(
            hotplug_wallpaper(Some(&entry("", CfgFitMode::Fill)), "", false),
            None
        );
    }

    #[test]
    fn hotplug_fit_mode_prefers_the_per_monitor_entry() {
        let entry = entry("mine.png", CfgFitMode::Center);

        assert_eq!(
            hotplug_fit_mode(Some(&entry), CfgFitMode::Stretch),
            CfgFitMode::Center
        );
    }

    #[test]
    fn hotplug_fit_mode_falls_back_to_the_global_one() {
        assert_eq!(
            hotplug_fit_mode(None, CfgFitMode::Stretch),
            CfgFitMode::Stretch
        );
    }
}
