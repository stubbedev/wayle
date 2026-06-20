/// Screenshot command definitions.
pub mod commands;
mod proxy;

use commands::ScreenshotCommands;

use self::proxy::{connect, format_error};
use super::CliAction;

/// Executes screenshot capture commands.
///
/// # Errors
/// Returns error if the command execution fails.
pub async fn execute(command: ScreenshotCommands) -> CliAction {
    let (mode, target) = match command {
        ScreenshotCommands::Region => ("region", String::new()),
        ScreenshotCommands::Output { name } => ("output", name.unwrap_or_default()),
        ScreenshotCommands::Window => ("window", String::new()),
    };

    let (_connection, proxy) = connect().await?;

    let path = proxy
        .capture(mode, &target)
        .await
        .map_err(|e| format_error("capture screenshot", e))?;

    if path.is_empty() {
        println!("Screenshot cancelled");
    } else {
        println!("Screenshot saved to {path}");
    }

    Ok(())
}
