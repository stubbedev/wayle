use super::proxy::{connect, format_error};
use crate::cli::CliAction;

/// Executes the status command.
///
/// # Errors
/// Returns error if D-Bus communication fails.
pub async fn execute() -> CliAction {
    let (_connection, proxy) = connect().await?;

    let active = proxy
        .active()
        .await
        .map_err(|e| format_error("get active state", e))?;

    if !active {
        println!("Idle");
        return Ok(());
    }

    let paused = proxy.paused().await.unwrap_or(false);
    let elapsed = proxy.elapsed().await.unwrap_or(0);
    let file = proxy.file().await.unwrap_or_default();

    let mins = elapsed / 60;
    let secs = elapsed % 60;
    let state = if paused { "Paused" } else { "Recording" };

    if file.is_empty() {
        println!("{state} ({mins}:{secs:02})");
    } else {
        println!("{state} ({mins}:{secs:02}) -> {file}");
    }

    Ok(())
}
