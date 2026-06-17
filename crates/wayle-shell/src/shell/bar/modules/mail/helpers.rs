use std::path::PathBuf;

use tokio::process::Command;

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
