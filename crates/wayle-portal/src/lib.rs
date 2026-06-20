//! Wayle's `xdg-desktop-portal` backend.
//!
//! Implements the `org.freedesktop.impl.portal.*` D-Bus interfaces that the
//! compositor-independent `xdg-desktop-portal` frontend routes sandboxed app
//! requests to. Run as a standalone process (`wayle portal`), D-Bus-activated
//! by the frontend via `org.freedesktop.impl.portal.desktop.wayle`.
//!
//! Unlike `wayle share-picker` (an xdg-desktop-portal-hyprland plugin that only
//! works under Hyprland), this backend plugs into the frontend directly, so it
//! works on niri, mango, Hyprland, sway, and any other Wayland compositor.
//!
//! # Interface coverage
//!
//! Implemented natively here: Settings, Lockdown (more land per phase:
//! ScreenCast, RemoteDesktop, Screenshot, GlobalShortcuts, Inhibit,
//! Notification, Wallpaper, Access). The generic GTK-dialog interfaces
//! (FileChooser, Print, …) are delegated to `xdg-desktop-portal-gtk` via
//! `portals.conf`.

mod error;
mod lockdown;
mod settings;

use std::future::pending;

use tracing::info;
use wayle_config::ConfigService;
use zbus::connection;

pub use self::error::Error;
use self::{lockdown::Lockdown, settings::Settings};

/// The backend's well-known D-Bus name (matches `wayle.portal`'s `DBusName`).
const BUS_NAME: &str = "org.freedesktop.impl.portal.desktop.wayle";

/// Connects to the session bus, mounts every implemented portal interface on
/// the portal root path, claims the backend name, and runs until the process
/// is terminated.
///
/// # Errors
///
/// Returns an error if the config cannot be loaded, the session bus is
/// unreachable, an interface cannot be registered, or the name is already
/// claimed by another backend.
pub async fn run() -> Result<(), Error> {
    let config = ConfigService::load()
        .await
        .map_err(|err| Error::Config(err.to_string()))?;

    let connection = connection::Builder::session()
        .map_err(|err| Error::Connection(err.to_string()))?
        .name(BUS_NAME)
        .map_err(|err| Error::NameRequest(err.to_string()))?
        .serve_at(settings::PORTAL_PATH, Settings::new(config.clone()))
        .map_err(|err| Error::Registration(err.to_string()))?
        .serve_at(settings::PORTAL_PATH, Lockdown)
        .map_err(|err| Error::Registration(err.to_string()))?
        .build()
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;

    settings::spawn_watcher(&connection, config);

    info!("Wayle portal backend registered at {BUS_NAME}");

    // Keep the connection (and thus the name + objects) alive forever.
    pending::<()>().await;
    Ok(())
}
