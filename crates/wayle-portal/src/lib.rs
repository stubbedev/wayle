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
//! Every `org.freedesktop.impl.portal.*` interface the frontend can route to a
//! backend is implemented natively: Access, Account, AppChooser, Background,
//! Clipboard, DynamicLauncher, Email, FileChooser, GlobalShortcuts, Inhibit,
//! InputCapture, Lockdown, Notification, Print, RemoteDesktop, ScreenCast,
//! Screenshot, Secret, Settings, Usb, and Wallpaper. Nothing is delegated to
//! `xdg-desktop-portal-gtk`.

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
mod manifest;
mod notification;
mod print;
mod protocol;
mod remotedesktop;
mod request;
mod response;
mod screencast;
mod screenshot;
mod secret;
mod session;
mod settings;
mod usb;
mod wallpaper;

use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use tracing::{info, warn};
use wayle_config::ConfigService;
use zbus::{
    Connection,
    fdo::{RequestNameFlags, RequestNameReply},
};

/// Shared registry mapping a ScreenCast PipeWire node id to its pixel size, so
/// RemoteDesktop can map absolute pointer coordinates onto the right extent.
pub(crate) type StreamSizes = Arc<Mutex<HashMap<u32, (u32, u32)>>>;

pub use self::error::Error;
use self::{
    access::Access, account::Account, appchooser::AppChooser, background::Background,
    clipboard::Clipboard, dynamiclauncher::DynamicLauncher, email::Email, filechooser::FileChooser,
    globalshortcuts::GlobalShortcuts, inhibit::Inhibit, inputcapture::InputCapture,
    lockdown::Lockdown, notification::Notification, print::Print, remotedesktop::RemoteDesktop,
    screencast::ScreenCast, screenshot::Screenshot, secret::Secret, settings::Settings, usb::Usb,
    wallpaper::WallpaperPortal,
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
    mount(&server, path, Settings::new(config.clone())).await?;
    mount(&server, path, Lockdown).await?;
    mount(
        &server,
        path,
        ScreenCast::new(connection.clone(), stream_sizes.clone()),
    )
    .await?;
    mount(&server, path, Screenshot::new(connection.clone())).await?;
    mount(
        &server,
        path,
        RemoteDesktop::new(connection.clone(), stream_sizes.clone()),
    )
    .await?;
    mount(&server, path, GlobalShortcuts::new(connection.clone())).await?;
    let notification = Notification::new(connection.clone());
    notification.spawn_action_forwarder();
    mount(&server, path, notification).await?;
    mount(&server, path, WallpaperPortal::new(connection.clone())).await?;
    mount(&server, path, Inhibit::new(connection.clone())).await?;
    mount(&server, path, Background::new()).await?;
    mount(&server, path, Usb::new()).await?;
    mount(&server, path, Clipboard::new(connection.clone())).await?;
    mount(&server, path, InputCapture::new(connection.clone())).await?;
    mount(&server, path, FileChooser::new(connection.clone())).await?;
    mount(&server, path, Email::new()).await?;
    mount(&server, path, Access::new(connection.clone())).await?;
    mount(&server, path, Account::new(connection.clone())).await?;
    mount(&server, path, AppChooser::new(connection.clone())).await?;
    mount(&server, path, DynamicLauncher::new(connection.clone())).await?;
    mount(&server, path, Print::new(connection.clone())).await?;
    mount(&server, path, Secret::new()).await?;

    // Request the name with `DoNotQueue` so that, if another backend already
    // owns it, we fail loudly instead of silently queueing and idling forever.
    let reply = connection
        .request_name_with_flags(BUS_NAME, RequestNameFlags::DoNotQueue.into())
        .await
        .map_err(|err| Error::NameRequest(err.to_string()))?;
    if reply != RequestNameReply::PrimaryOwner {
        return Err(Error::NameRequest(format!(
            "{BUS_NAME} is already owned by another backend (reply: {reply:?})"
        )));
    }

    settings::spawn_watcher(&connection, config);

    info!("Wayle portal backend registered at {BUS_NAME}");

    // Run until terminated. On SIGTERM/SIGINT, clear live sessions so their
    // PipeWire loops stop, then return cleanly.
    wait_for_shutdown().await;
    info!("Wayle portal backend shutting down");
    session::clear_all();

    Ok(())
}

/// Registers one portal interface object at `path`, mapping a registration
/// failure to [`Error::Registration`]. Collapses the otherwise-repeated
/// `server.at(...).await.map_err(...)?` boilerplate at every mount site.
async fn mount<I>(
    server: &zbus::object_server::ObjectServer,
    path: &str,
    iface: I,
) -> Result<(), Error>
where
    I: zbus::object_server::Interface,
{
    server
        .at(path, iface)
        .await
        .map(|_| ())
        .map_err(|err| Error::Registration(err.to_string()))
}

/// Resolves on the first SIGTERM or SIGINT.
///
/// On non-Unix targets (none are supported in practice) this falls back to
/// `ctrl_c` alone.
async fn wait_for_shutdown() {
    #[cfg(unix)]
    {
        use tokio::signal::unix::{SignalKind, signal};

        let mut term = match signal(SignalKind::terminate()) {
            Ok(term) => term,
            Err(err) => {
                warn!("cannot install SIGTERM handler: {err}; waiting on SIGINT only");
                let _ = tokio::signal::ctrl_c().await;
                return;
            }
        };
        tokio::select! {
            res = tokio::signal::ctrl_c() => {
                if let Err(err) = res {
                    warn!("cannot wait on SIGINT: {err}");
                }
            }
            _ = term.recv() => {}
        }
    }

    #[cfg(not(unix))]
    {
        let _ = tokio::signal::ctrl_c().await;
    }
}
