//! `wayle toast` — show a custom on-screen toast via the widget socket.

use super::CliAction;

/// Sends a custom toast to the running shell.
///
/// Either `label` or `preset` must be supplied. A preset (`[[toasts.presets]]`)
/// provides defaults that the explicit arguments override.
///
/// # Errors
/// Returns an error when neither a label nor a preset is given, or when the
/// shell's widget socket is unreachable or the request is rejected.
pub async fn execute(
    label: Option<&str>,
    icon: Option<&str>,
    percentage: Option<f64>,
    duration: Option<u32>,
    preset: Option<&str>,
    class: Option<&str>,
) -> CliAction {
    if label.is_none() && preset.is_none() {
        return Err(String::from("a toast needs a label or --preset"));
    }

    wayle_ipc::widget_socket::send_toast(label, icon, percentage, duration, preset, class)
        .await
        .map_err(|err| err.to_string())
}
