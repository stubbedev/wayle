use std::process::Stdio;

use tokio::process::Command;
use tracing::warn;
use wayle_config::schemas::modules::MailConfig;

/// Render the label format, substituting `{{ count }}`.
pub(super) fn format_label(format: &str, count: u32) -> String {
    format.replace("{{ count }}", &count.to_string())
}

/// Substitute `{{ count }}` (total unread) and `{{ new }}` (newly arrived) in a
/// notification format string.
fn render_notification(format: &str, count: u32, new: u32) -> String {
    format
        .replace("{{ count }}", &count.to_string())
        .replace("{{ new }}", &new.to_string())
}

/// Fire a fire-and-forget `notify-send` reporting newly arrived mail, using the
/// module icon. `new` is how many messages arrived since the last count.
pub(super) fn fire_new_mail_notification(config: &MailConfig, count: u32, new: u32) {
    let summary = render_notification(&config.notify_summary.get(), count, new);
    let body = render_notification(&config.notify_body.get(), count, new);
    let icon = config.icon_name.get();

    let mut command = Command::new("notify-send");
    command
        .arg("--app-name=Wayle")
        .arg(format!("--icon={icon}"))
        .arg(summary)
        .arg(body)
        .stdout(Stdio::null())
        .stderr(Stdio::null());

    match command.spawn() {
        Ok(child) => {
            tokio::spawn(async move {
                let _ = child.wait_with_output().await;
            });
        }
        Err(err) => warn!(error = %err, "cannot spawn notify-send for mail"),
    }
}
