//! Lock command: locks the session via Wayle's lock screen.

use wayle_ipc::shell_ipc::ShellIpcProxy;
use zbus::Connection;

use crate::cli::CliAction;

/// Locks the session by calling the shell's `Lock` D-Bus method.
///
/// # Errors
///
/// Returns an error if the session bus is unavailable, the shell is not
/// running, or the lock screen is not ready.
pub async fn execute() -> CliAction {
    let connection = Connection::session()
        .await
        .map_err(|err| format!("D-Bus session unavailable: {err}"))?;

    let proxy = ShellIpcProxy::new(&connection)
        .await
        .map_err(|err| format!("cannot create shell IPC proxy: {err}"))?;

    proxy
        .lock()
        .await
        .map_err(|err| format!("lock failed: {err}"))?;

    println!("Session locked");
    Ok(())
}
