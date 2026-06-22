//! `wayle share-picker` — the xdg-desktop-portal-hyprland custom screencast
//! picker.
//!
//! The portal execs this with the `XDPH_WINDOW_SHARING_LIST` environment
//! variable set, then reads the chosen source from stdout. This stub forwards
//! the request to the running shell over D-Bus (which shows the picker
//! surface) and prints the returned selection line.
//!
//! Only the `[SELECTION]...` line is ever written to stdout; everything else
//! goes to stderr so the portal's parser is not corrupted.

use wayle_ipc::share_picker::SharePickerProxy;

use crate::cli::dbus;

/// Runs the picker stub. Returns the process exit code.
///
/// Prints `[SELECTION]<suffix>` on selection, nothing on cancel.
pub async fn execute(allow_token: bool) -> i32 {
    let window_list = std::env::var("XDPH_WINDOW_SHARING_LIST").unwrap_or_default();

    let connection = match dbus::session().await {
        Ok(connection) => connection,
        Err(err) => {
            eprintln!("{err}");
            return 1;
        }
    };

    let proxy = match SharePickerProxy::new(&connection).await {
        Ok(proxy) => proxy,
        Err(err) => {
            eprintln!("Failed to create share picker proxy: {err}");
            return 1;
        }
    };

    // Legacy single-select CLI path: never multi-select.
    match proxy.pick(&window_list, allow_token, false).await {
        Ok(selection) if !selection.is_empty() => {
            println!("[SELECTION]{selection}");
            0
        }
        // Empty selection means the user cancelled; emit nothing.
        Ok(_) => 0,
        Err(err) => {
            eprintln!(
                "{}",
                dbus::format_error("SharePicker", "open share picker", err)
            );
            1
        }
    }
}
