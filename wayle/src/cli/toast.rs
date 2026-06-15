//! `wayle toast` — show a custom on-screen toast via the widget socket.

use super::CliAction;

/// Sends a custom toast to the running shell.
///
/// # Errors
/// Returns an error if the shell's widget socket is unreachable or the request
/// is rejected.
pub async fn execute(
    label: &str,
    icon: Option<&str>,
    percentage: Option<f64>,
    duration: Option<u32>,
) -> CliAction {
    wayle_ipc::widget_socket::send_toast(label, icon, percentage, duration)
        .await
        .map_err(|err| err.to_string())
}
