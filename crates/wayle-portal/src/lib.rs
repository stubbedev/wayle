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
//! Implemented natively: Settings, Lockdown, ScreenCast (more land per phase:
//! RemoteDesktop, Screenshot, GlobalShortcuts, Inhibit, Notification,
//! Wallpaper, Access). The generic GTK-dialog interfaces (FileChooser, Print,
//! …) are delegated to `xdg-desktop-portal-gtk` via `portals.conf`.

mod access;
mod account;
mod appchooser;
mod background;
mod clipboard;
mod dbus_util;
mod dynamiclauncher;
mod email;
mod error;
mod filechooser;
mod globalshortcuts;
mod inhibit;
mod inputcapture;
mod lockdown;
mod notification;
mod print;
mod protocol;
mod remotedesktop;
mod response;
mod screencast;
mod screenshot;
mod session;
mod settings;
mod usb;
mod wallpaper;

use std::{
    collections::HashMap,
    future::pending,
    sync::{Arc, Mutex},
};

use tracing::info;
use wayle_config::ConfigService;
use zbus::Connection;

/// Shared registry mapping a ScreenCast PipeWire node id to its pixel size, so
/// RemoteDesktop can map absolute pointer coordinates onto the right extent.
pub(crate) type StreamSizes = Arc<Mutex<HashMap<u32, (u32, u32)>>>;

pub use self::error::Error;
use self::{
    access::Access, account::Account, appchooser::AppChooser, background::Background,
    clipboard::Clipboard, dynamiclauncher::DynamicLauncher, email::Email,
    filechooser::FileChooser, globalshortcuts::GlobalShortcuts, inhibit::Inhibit,
    inputcapture::InputCapture, lockdown::Lockdown, notification::Notification, print::Print,
    remotedesktop::RemoteDesktop, screencast::ScreenCast, screenshot::Screenshot,
    settings::Settings, usb::Usb, wallpaper::WallpaperPortal,
};

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
#[allow(clippy::cognitive_complexity)]
pub async fn run() -> Result<(), Error> {
    let config = ConfigService::load()
        .await
        .map_err(|err| Error::Config(err.to_string()))?;

    let connection = Connection::session()
        .await
        .map_err(|err| Error::Connection(err.to_string()))?;

    let stream_sizes: StreamSizes = Arc::new(Mutex::new(HashMap::new()));

    let server = connection.object_server();
    let path = settings::PORTAL_PATH;
    server
        .at(path, Settings::new(config.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Lockdown)
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, ScreenCast::new(connection.clone(), stream_sizes.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Screenshot::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, RemoteDesktop::new(connection.clone(), stream_sizes.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, GlobalShortcuts::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    let notification = Notification::new(connection.clone());
    notification.spawn_action_forwarder();
    server
        .at(path, notification)
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, WallpaperPortal::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Inhibit::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Background::new())
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Usb::new())
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Clipboard::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, InputCapture::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, FileChooser::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Email::new())
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Access::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Account::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, AppChooser::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, DynamicLauncher::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;
    server
        .at(path, Print::new(connection.clone()))
        .await
        .map_err(|err| Error::Registration(err.to_string()))?;

    connection
        .request_name(BUS_NAME)
        .await
        .map_err(|err| Error::NameRequest(err.to_string()))?;

    settings::spawn_watcher(&connection, config);

    info!("Wayle portal backend registered at {BUS_NAME}");

    // Keep the connection (and thus the name + objects) alive forever.
    pending::<()>().await;
    Ok(())
}
