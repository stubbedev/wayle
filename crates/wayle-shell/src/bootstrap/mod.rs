//! Application bootstrap: service initialization and instance detection.

mod wallpaper;
mod weather;

use std::{
    error::Error,
    fmt::Display,
    sync::Arc,
    time::{Duration, Instant},
};

use tokio::task::JoinHandle;
use tracing::{debug, info, warn};
use wayle_audio::AudioService;
use wayle_battery::BatteryService;
use wayle_bluetooth::BluetoothService;
use wayle_brightness::BrightnessService;
use wayle_config::ConfigService;
use wayle_core::{DeferredService, Property};
use wayle_hyprland::HyprlandService;
use wayle_ipc::shell::APP_ID;
use wayle_mango::MangoService;
use wayle_media::MediaService;
use wayle_network::NetworkService;
use wayle_niri::NiriService;
use wayle_notification::NotificationService;
use wayle_power_profiles::PowerProfilesService;
use wayle_sway::SwayService;
use wayle_sysinfo::SysinfoService;
use wayle_systray::{SystemTrayService, types::TrayMode};
use wayle_treeman::TreemanService;
use wayle_wallpaper::WallpaperService;
use zbus::{Connection, fdo::DBusProxy};

use crate::{
    services::{
        IdleInhibitService, RecorderService, ShellIpcService, ToastBus, WidgetBus, screenshot,
        share_picker, widget_ipc,
    },
    shell::ShellServices,
    startup::StartupTimer,
    watchers::build_extractor_config,
};

async fn spawned<T, E: Display>(handle: JoinHandle<Result<T, E>>) -> Result<T, String> {
    match handle.await {
        Ok(Ok(val)) => Ok(val),
        Ok(Err(err)) => Err(err.to_string()),
        Err(join_err) => Err(join_err.to_string()),
    }
}

macro_rules! try_service {
    ($timer:expr, $name:literal, $future:expr) => {
        match $timer.time($name, $future).await {
            Ok(service) => Some(Arc::new(service)),
            Err(e) => {
                warn!(error = %e, concat!($name, " unavailable"));
                None
            }
        }
    };
    ($timer:expr, $name:literal, $future:expr, no_wrap) => {
        match $timer.time($name, $future).await {
            Ok(service) => Some(service),
            Err(e) => {
                warn!(error = %e, concat!($name, " unavailable"));
                None
            }
        }
    };
}

struct CoreServices {
    battery: Option<Arc<BatteryService>>,
    brightness: Option<Arc<BrightnessService>>,
    idle_inhibit: Arc<IdleInhibitService>,
    network: Option<Arc<NetworkService>>,
    sysinfo: Arc<SysinfoService>,
    wallpaper: Option<Arc<WallpaperService>>,
}

struct DaemonServices {
    audio: Option<Arc<AudioService>>,
    media: Option<Arc<MediaService>>,
    notification: Option<Arc<NotificationService>>,
    systray: Option<Arc<SystemTrayService>>,
}

struct OptionalServices {
    hyprland: Option<Arc<HyprlandService>>,
    mango: Option<Arc<MangoService>>,
    niri: Option<Arc<NiriService>>,
    sway: Option<Arc<SwayService>>,
}

pub async fn is_already_running() -> bool {
    let start = Instant::now();

    let Ok(connection) = Connection::session().await else {
        return false;
    };

    let Ok(dbus) = DBusProxy::new(&connection).await else {
        return false;
    };

    let Ok(name) = APP_ID.try_into() else {
        return false;
    };

    let result = dbus.name_has_owner(name).await.unwrap_or(false);
    debug!(
        duration_ms = start.elapsed().as_millis() as u64,
        "DBus instance check"
    );
    result
}

#[allow(clippy::cognitive_complexity)]
pub async fn init_services() -> Result<(StartupTimer, ShellServices), Box<dyn Error>> {
    let mut timer = StartupTimer::new();

    // Hooks for the window components that stay in this crate while their
    // callers (dropdown click builtins, shell IPC) live in wayle-shell-core.
    wayle_shell_core::bar::dropdown_registry::set_screenshot_trigger(screenshot_trigger);
    wayle_shell_core::services::shell_ipc::set_lock_trigger(crate::services::lock::lock);
    wayle_shell_core::bar::set_power_menu_trigger(|| {
        crate::services::power_menu::show();
    });

    let config_service = timer.time("Config", ConfigService::load()).await?;

    let bluetooth: DeferredService<BluetoothService> = DeferredService::new(None);
    let power_profiles: DeferredService<PowerProfilesService> = DeferredService::new(None);

    let (weather, core, daemons, optional) = {
        let config = config_service.config();
        let weather = timer.time_sync("Weather", || {
            weather::build_weather_service(&config.modules)
        });

        let (core, daemons, optional) = tokio::join!(
            init_core_services(&timer, config),
            init_daemon_services(&timer, &config.modules),
            init_optional_services(&timer),
        );

        (weather, core?, daemons, optional)
    };

    spawn_deferred_bluetooth(bluetooth.clone());
    spawn_deferred_power_profiles(power_profiles.clone());

    let shell_ipc = match ShellIpcService::new().await {
        Ok(service) => Arc::new(service),
        Err(err) => {
            warn!(error = %err, "Shell IPC service unavailable");
            return Err(err.into());
        }
    };

    let (widget_bus, toast_bus) = init_widget_socket().await;

    init_share_picker().await;

    init_screenshot().await;

    init_file_chooser().await;

    init_portal_dialogs().await;

    init_print().await;

    let recorder = init_recorder(config_service.clone(), toast_bus.clone()).await;

    let mail = crate::services::MailService::new(config_service.clone());

    let treeman = timer.time_sync("Treeman", || Arc::new(TreemanService::builder().build()));

    timer.mark_services_done();

    let services = ShellServices {
        audio: daemons.audio,
        battery: core.battery,
        bluetooth,
        brightness: core.brightness,
        config: config_service,
        hyprland: optional.hyprland,
        power_profiles,
        idle_inhibit: core.idle_inhibit,
        mail,
        recorder,
        mango: optional.mango,
        media: daemons.media,
        niri: optional.niri,
        sway: optional.sway,
        network: core.network,
        notification: daemons.notification,
        sysinfo: core.sysinfo,
        systray: daemons.systray,
        wallpaper: core.wallpaper,
        weather,
        treeman,
        shell_ipc,
        widget_bus,
        toast_bus,
    };

    Ok((timer, services))
}

/// Registers the share picker D-Bus service. Non-fatal: a failure just leaves
/// the shell usable without the custom screencast picker.
async fn init_share_picker() {
    if let Err(err) = share_picker::start().await {
        warn!(error = %err, "Share picker service unavailable");
    }
}

/// Registers the screenshot D-Bus service. Non-fatal: a failure just leaves the
/// shell usable without `wayle screenshot`.
async fn init_screenshot() {
    if let Err(err) = screenshot::start().await {
        warn!(error = %err, "Screenshot service unavailable");
    }
}

/// Registers the file chooser D-Bus service. Non-fatal: a failure just leaves
/// the shell usable without the native portal file dialog.
async fn init_file_chooser() {
    if let Err(err) = crate::services::file_chooser::start().await {
        warn!(error = %err, "File chooser service unavailable");
    }
}

/// Registers the portal dialog D-Bus service (access/account/appchooser/launcher).
async fn init_portal_dialogs() {
    if let Err(err) = crate::services::portal_dialogs::start().await {
        warn!(error = %err, "Portal dialogs service unavailable");
    }
}

/// Registers the print D-Bus service. Non-fatal.
async fn init_print() {
    if let Err(err) = crate::services::print::start().await {
        warn!(error = %err, "Print service unavailable");
    }
}

/// Initializes the recorder service, returning `None` (non-fatal) if GStreamer
/// or the D-Bus registration is unavailable.
async fn init_recorder(
    config: Arc<ConfigService>,
    toast_bus: ToastBus,
) -> Option<Arc<RecorderService>> {
    match RecorderService::new(config, toast_bus).await {
        Ok(service) => Some(Arc::new(service)),
        Err(err) => {
            warn!(error = %err, "Recorder service unavailable");
            None
        }
    }
}

/// Creates the widget + toast buses and starts the shared unix-socket listener.
///
/// A socket failure is logged but non-fatal: the buses still exist so widgets
/// and the OSD subscribe cleanly; only external updates are unavailable.
async fn init_widget_socket() -> (WidgetBus, ToastBus) {
    let widget_bus = WidgetBus::new();
    let toast_bus = ToastBus::new();
    if let Err(err) = widget_ipc::start(widget_bus.clone(), toast_bus.clone()).await {
        warn!(error = %err, "Widget socket unavailable; external widget/toast updates disabled");
    }
    (widget_bus, toast_bus)
}

async fn init_core_services(
    timer: &StartupTimer,
    config: &wayle_config::Config,
) -> Result<CoreServices, Box<dyn Error>> {
    let modules = &config.modules;

    let theming_monitor = config.styling.theming_monitor.get();
    let theming_monitor = if theming_monitor.is_empty() {
        None
    } else {
        Some(theming_monitor)
    };
    let color_extractor = build_extractor_config(&config.styling);

    let sysinfo = Arc::new(timer.time_sync("Sysinfo", || {
        SysinfoService::builder()
            .cpu_interval(Duration::from_millis(modules.cpu.poll_interval_ms.get()))
            .memory_interval(Duration::from_millis(modules.ram.poll_interval_ms.get()))
            .disk_interval(Duration::from_millis(
                modules.storage.poll_interval_ms.get(),
            ))
            .network_interval(Duration::from_millis(
                modules.netstat.poll_interval_ms.get(),
            ))
            .build()
    }));

    let startup_duration = modules.idle_inhibit.startup_duration.get();

    let battery_task = tokio::spawn(BatteryService::new());
    let brightness_external = modules.brightness.enable_external.get();
    let brightness_task = tokio::spawn(async move {
        BrightnessService::builder()
            .external_monitors(brightness_external)
            .build()
            .await
    });
    let network_task = tokio::spawn(NetworkService::new());
    let wallpaper_cfg = config.wallpaper.clone();
    let wallpaper_task = tokio::spawn(async move {
        wallpaper::build_wallpaper_service(&wallpaper_cfg, theming_monitor, color_extractor).await
    });
    let idle_inhibit_task = tokio::spawn(IdleInhibitService::new(startup_duration));

    let (battery, brightness, network, wallpaper, idle_inhibit) = tokio::join!(
        async { try_service!(timer, "Battery", spawned(battery_task)) },
        async { try_service!(timer, "Brightness", spawned(brightness_task), no_wrap) },
        async { try_service!(timer, "Network", spawned(network_task)) },
        async { try_service!(timer, "Wallpaper", spawned(wallpaper_task), no_wrap) },
        timer.time("IdleInhibit", spawned(idle_inhibit_task)),
    );

    Ok(CoreServices {
        battery,
        brightness: brightness.flatten(),
        idle_inhibit: Arc::new(idle_inhibit?),
        network,
        sysinfo,
        wallpaper,
    })
}

async fn init_optional_services(timer: &StartupTimer) -> OptionalServices {
    let hyprland_task = tokio::spawn(HyprlandService::new());
    let mango_task = tokio::spawn(MangoService::new());
    let niri_task = tokio::spawn(NiriService::new());
    let sway_task = tokio::spawn(SwayService::new());

    let (hyprland, mango, niri, sway) = tokio::join!(
        timer.time("Hyprland", spawned(hyprland_task)),
        timer.time("Mango", spawned(mango_task)),
        timer.time("Niri", spawned(niri_task)),
        timer.time("Sway", spawned(sway_task)),
    );

    OptionalServices {
        hyprland: hyprland.ok(),
        mango: mango.ok(),
        niri: niri.ok(),
        sway: sway.ok(),
    }
}

fn spawn_deferred_bluetooth(property: DeferredService<BluetoothService>) {
    tokio::spawn(async move {
        let start = Instant::now();

        match BluetoothService::new().await {
            Ok(service) => {
                let duration_ms = start.elapsed().as_millis() as u64;
                info!(duration_ms, "Bluetooth ready (deferred)");
                property.replace(Some(Arc::new(service)));
            }
            Err(err) => {
                warn!(error = %err, "Bluetooth unavailable");
            }
        }
    });
}

fn spawn_deferred_power_profiles(property: DeferredService<PowerProfilesService>) {
    tokio::spawn(async move {
        let start = Instant::now();

        match PowerProfilesService::builder().with_daemon().build().await {
            Ok(service) => {
                let duration_ms = start.elapsed().as_millis() as u64;
                info!(duration_ms, "PowerProfiles ready (deferred)");
                property.replace(Some(service));
            }
            Err(err) => {
                warn!(error = %err, "PowerProfiles unavailable");
            }
        }
    });
}

async fn init_daemon_services(
    timer: &StartupTimer,
    modules: &wayle_config::schemas::modules::ModulesConfig,
) -> DaemonServices {
    let ignored = modules.media.players_ignored.get().clone();
    let priority = modules.media.player_priority.get().clone();

    let audio_task = tokio::spawn(AudioService::builder().with_daemon().build());
    let media_task = tokio::spawn(
        MediaService::builder()
            .with_daemon()
            .with_art_cache()
            .ignored_players(ignored)
            .priority_players(priority)
            .build(),
    );
    let systray_task = tokio::spawn(
        SystemTrayService::builder()
            .with_daemon()
            .mode(TrayMode::Auto)
            .build(),
    );

    // Conditionally initialize notification service
    let notification = if modules.notifications.enabled.get() {
        let blocklist = Property::new(modules.notifications.blocklist.get());
        let notification_task = tokio::spawn(
            NotificationService::builder()
                .with_daemon()
                .blocklist(blocklist)
                .build(),
        );
        try_service!(timer, "Notification", spawned(notification_task), no_wrap)
    } else {
        None
    };

    let (audio, media, systray) = tokio::join!(
        async { try_service!(timer, "Audio", spawned(audio_task), no_wrap) },
        async { try_service!(timer, "Media", spawned(media_task), no_wrap) },
        async { try_service!(timer, "SystemTray", spawned(systray_task), no_wrap) },
    );

    DaemonServices {
        audio,
        media,
        notification,
        systray,
    }
}

/// `wayle screenshot …` click builtin: forwards to the screenshot window
/// component hosted in this crate.
fn screenshot_trigger(mode: String, target: String) -> bool {
    let Some(sender) = crate::services::screenshot::host_sender() else {
        return false;
    };
    let (reply, _rx) = tokio::sync::oneshot::channel();
    sender.emit(crate::shell::screenshot::ScreenshotInput::Capture {
        mode,
        target,
        reply,
    });
    true
}
