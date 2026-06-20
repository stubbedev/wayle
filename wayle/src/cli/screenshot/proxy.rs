use wayle_ipc::screenshot::ScreenshotProxy;
use zbus::{Connection, Error as ZbusError};

use crate::cli::dbus;

const SERVICE_NAME: &str = "Screenshot";

/// Creates a `ScreenshotProxy` connection.
///
/// # Errors
/// Returns error if D-Bus connection or proxy creation fails.
pub async fn connect() -> Result<(Connection, ScreenshotProxy<'static>), String> {
    let connection = dbus::session().await?;

    let proxy = ScreenshotProxy::new(&connection)
        .await
        .map_err(|e| format!("Failed to create screenshot proxy: {e}"))?;

    Ok((connection, proxy))
}

/// Transforms zbus errors into user-friendly messages.
pub fn format_error(operation: &str, error: ZbusError) -> String {
    dbus::format_error(SERVICE_NAME, operation, error)
}
