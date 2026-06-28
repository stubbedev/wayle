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

use crate::cli::{dbus, portal::commands::PortalDialog};

/// Shows the requested dialog and prints its result. Returns `Err` with a
/// user-facing message if the D-Bus call fails.
pub async fn execute(dialog: PortalDialog) -> Result<(), String> {
    let connection = dbus::session().await?;

    match dialog {
        PortalDialog::FileChooser {
            save,
            multiple,
            directory,
        } => {
            let proxy = FileChooserProxy::new(&connection)
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
        }
        PortalDialog::Screenshot { mode, target } => {
            let proxy = ScreenshotProxy::new(&connection)
                .await
                .map_err(|e| format!("Failed to create screenshot proxy: {e}"))?;
            let path = proxy
                .capture(&mode, &target)
                .await
                .map_err(|e| dbus::format_error("Screenshot", "capture screenshot", e))?;
            print_result(&path);
        }
        PortalDialog::Color => {
            let proxy = ScreenshotProxy::new(&connection)
                .await
                .map_err(|e| format!("Failed to create screenshot proxy: {e}"))?;
            let (r, g, b) = proxy
                .pick_color()
                .await
                .map_err(|e| dbus::format_error("Screenshot", "pick color", e))?;
            println!("rgb({r:.3}, {g:.3}, {b:.3})");
        }
        PortalDialog::ScreenCast {
            allow_token,
            multiple,
        } => {
            let proxy = SharePickerProxy::new(&connection)
                .await
                .map_err(|e| format!("Failed to create share picker proxy: {e}"))?;
            // Empty window list: preview the picker with no pre-seeded sources.
            let selection = proxy
                .pick("", allow_token, multiple)
                .await
                .map_err(|e| dbus::format_error("SharePicker", "show share picker", e))?;
            print_result(&selection);
        }
        PortalDialog::Print => {
            let proxy = PrintProxy::new(&connection)
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
        }
        PortalDialog::Access => {
            let proxy = PortalDialogsProxy::new(&connection)
                .await
                .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
            let granted = proxy
                .access(
                    "Preview: Access",
                    "An application is requesting access",
                    "This is a preview of the generic access prompt.",
                    "Allow",
                    "Deny",
                )
                .await
                .map_err(|e| dbus::format_error("Access", "show access prompt", e))?;
            println!("{}", if granted { "granted" } else { "denied" });
        }
        PortalDialog::Account => {
            let proxy = PortalDialogsProxy::new(&connection)
                .await
                .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
            let shared = proxy
                .account("This is a preview of the account-sharing consent prompt.")
                .await
                .map_err(|e| dbus::format_error("Account", "show account prompt", e))?;
            println!("{}", if shared { "shared" } else { "declined" });
        }
        PortalDialog::AppChooser => {
            let proxy = PortalDialogsProxy::new(&connection)
                .await
                .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
            let chosen = proxy
                .choose_application(vec![], "text/plain", "")
                .await
                .map_err(|e| dbus::format_error("AppChooser", "show app chooser", e))?;
            print_result(&chosen);
        }
        PortalDialog::DynamicLauncher => {
            let proxy = PortalDialogsProxy::new(&connection)
                .await
                .map_err(|e| format!("Failed to create portal dialogs proxy: {e}"))?;
            let approved = proxy
                .confirm_install("Preview Launcher", "application-x-executable")
                .await
                .map_err(|e| {
                    dbus::format_error("DynamicLauncher", "show install confirmation", e)
                })?;
            println!("{}", if approved { "approved" } else { "rejected" });
        }
    }

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
