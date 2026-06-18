use std::{path::PathBuf, process::Stdio};

use tokio::process::Command;
use tracing::warn;
use wayle_config::schemas::modules::MailConfig;

/// Run `notmuch count <query>` and parse the result. Returns 0 on any failure
/// (notmuch missing, DB unconfigured, malformed output).
pub(super) async fn query_count(query: &str) -> u32 {
    let output = Command::new("notmuch")
        .arg("count")
        .arg(query)
        .output()
        .await;

    let Ok(output) = output else {
        return 0;
    };
    if !output.status.success() {
        return 0;
    }

    String::from_utf8_lossy(&output.stdout)
        .trim()
        .parse()
        .unwrap_or(0)
}

/// Resolve the notmuch maildir (`notmuch config get database.path`) to watch.
pub(super) async fn maildir_path() -> Option<PathBuf> {
    let output = Command::new("notmuch")
        .args(["config", "get", "database.path"])
        .output()
        .await
        .ok()?;

    if !output.status.success() {
        return None;
    }

    let path = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if path.is_empty() {
        None
    } else {
        Some(PathBuf::from(path))
    }
}

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
