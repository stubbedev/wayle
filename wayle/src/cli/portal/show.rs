//! `wayle portal show <dialog>` — pop a portal dialog without an application
//! request, for developing and eyeballing the dialog UIs.
//!
//! Each variant calls the same shell-side D-Bus service the real portal backend
//! delegates to, with placeholder arguments. The chosen result (or a
//! `cancelled` line) is printed to stdout. Requires `wayle shell` to be running.

use wayle_ipc::{
    file_chooser::FileChooserProxy, portal_dialogs::PortalDialogsProxy, print::PrintProxy,
    screenshot::ScreenshotProxy, share_picker::SharePickerProxy,
};
use zbus::Connection;

use crate::cli::{dbus, portal::commands::PortalDialog};

/// Shows the requested dialog and prints its result.
///
/// # Errors
///
/// Returns `Err` with a user-facing message if the session bus is unavailable
/// or the dialog's D-Bus call fails.
pub async fn execute(dialog: PortalDialog) -> Result<(), String> {
    let conn = dbus::session().await?;
    match dialog {
        PortalDialog::FileChooser {
            save,
            multiple,
            directory,
        } => file_chooser(&conn, save, multiple, directory).await,
        PortalDialog::Screenshot { mode, target } => screenshot(&conn, &mode, &target).await,
        PortalDialog::Color => color(&conn).await,
        PortalDialog::ScreenCast {
            allow_token,
            multiple,
        } => screen_cast(&conn, allow_token, multiple).await,
        PortalDialog::Print => print_dialog(&conn).await,
        PortalDialog::Access => access(&conn).await,
        PortalDialog::Account => account(&conn).await,
        PortalDialog::AppChooser => app_chooser(&conn).await,
        PortalDialog::DynamicLauncher => dynamic_launcher(&conn).await,
        PortalDialog::Wallpaper { uri } => wallpaper(&conn, &uri).await,
    }
}

async fn file_chooser(
    conn: &Connection,
    save: bool,
    multiple: bool,
    directory: bool,
) -> Result<(), String> {
    let proxy = FileChooserProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create file chooser proxy: {e}"))?;
    let uris = if save {
        proxy
            .save_file("Preview: Save File", "untitled.txt", vec![], "")
            .await
    } else {
        proxy
            .open_file("Preview: Open File", multiple, directory, vec![], "")
            .await
    }
    .map_err(|e| dbus::format_error("FileChooser", "show file chooser", e))?;
    print_uris(&uris);
    Ok(())
}

async fn screenshot(conn: &Connection, mode: &str, target: &str) -> Result<(), String> {
    let proxy = ScreenshotProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create screenshot proxy: {e}"))?;
    let path = proxy
        .capture(mode, target)
        .await
        .map_err(|e| dbus::format_error("Screenshot", "capture screenshot", e))?;
    print_result(&path);
    Ok(())
}

async fn color(conn: &Connection) -> Result<(), String> {
    let proxy = ScreenshotProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create screenshot proxy: {e}"))?;
    let (r, g, b) = proxy
        .pick_color()
        .await
        .map_err(|e| dbus::format_error("Screenshot", "pick color", e))?;
    println!("rgb({r:.3}, {g:.3}, {b:.3})");
    Ok(())
}

async fn screen_cast(conn: &Connection, allow_token: bool, multiple: bool) -> Result<(), String> {
    let proxy = SharePickerProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create share picker proxy: {e}"))?;
    // Empty window list: preview the picker with no pre-seeded sources.
    let selection = proxy
        .pick("", allow_token, multiple)
        .await
        .map_err(|e| dbus::format_error("SharePicker", "show share picker", e))?;
    print_result(&selection);
    Ok(())
}

async fn print_dialog(conn: &Connection) -> Result<(), String> {
    let proxy = PrintProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create print proxy: {e}"))?;
    let (granted, settings, token) = proxy
        .prepare("Preview: Print")
        .await
        .map_err(|e| dbus::format_error("Print", "show print dialog", e))?;
    if granted {
        println!("prepared (token {token}, {} settings)", settings.len());
    } else {
        println!("cancelled");
    }
    Ok(())
}

async fn access(conn: &Connection) -> Result<(), String> {
    let proxy = PortalDialogsProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
    let granted = proxy
        .access(
            "Preview: Access",
            "An application is requesting access",
            "This is a preview of the generic access prompt.",
            "Allow",
            "Deny",
            "dialog-password-symbolic",
        )
        .await
        .map_err(|e| dbus::format_error("Access", "show access prompt", e))?;
    println!("{}", if granted { "granted" } else { "denied" });
    Ok(())
}

async fn account(conn: &Connection) -> Result<(), String> {
    let proxy = PortalDialogsProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
    let shared = proxy
        .account("This is a preview of the account-sharing consent prompt.")
        .await
        .map_err(|e| dbus::format_error("Account", "show account prompt", e))?;
    println!("{}", if shared { "shared" } else { "declined" });
    Ok(())
}

async fn app_chooser(conn: &Connection) -> Result<(), String> {
    let proxy = PortalDialogsProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
    let chosen = proxy
        .choose_application(vec![], "text/plain", "")
        .await
        .map_err(|e| dbus::format_error("AppChooser", "show app chooser", e))?;
    print_result(&chosen);
    Ok(())
}

async fn dynamic_launcher(conn: &Connection) -> Result<(), String> {
    let proxy = PortalDialogsProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
    let approved = proxy
        .confirm_install("Preview Launcher", "application-x-executable")
        .await
        .map_err(|e| dbus::format_error("DynamicLauncher", "show install confirmation", e))?;
    println!("{}", if approved { "approved" } else { "rejected" });
    Ok(())
}

async fn wallpaper(conn: &Connection, uri: &str) -> Result<(), String> {
    let proxy = PortalDialogsProxy::new(conn)
        .await
        .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
    let accepted = proxy
        .confirm_wallpaper(uri)
        .await
        .map_err(|e| dbus::format_error("Wallpaper", "show wallpaper preview", e))?;
    println!("{}", if accepted { "accepted" } else { "declined" });
    Ok(())
}

/// Prints each returned URI, or `cancelled` when the list is empty.
fn print_uris(uris: &[String]) {
    if uris.is_empty() {
        println!("cancelled");
    } else {
        for uri in uris {
            println!("{uri}");
        }
    }
}

/// Prints a single result string, or `cancelled` when it is empty.
fn print_result(result: &str) {
    if result.is_empty() {
        println!("cancelled");
    } else {
        println!("{result}");
    }
}
