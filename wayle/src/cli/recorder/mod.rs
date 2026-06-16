/// Recorder command definitions.
pub mod commands;
mod pause;
mod proxy;
mod resume;
mod start;
/// Status command implementation.
pub mod status;
mod stop;
mod toggle;

use commands::RecorderCommands;

use super::CliAction;

/// Executes screen recorder control commands.
///
/// # Errors
/// Returns error if the command execution fails.
pub async fn execute(command: RecorderCommands) -> CliAction {
    match command {
        RecorderCommands::Start => start::execute().await,
        RecorderCommands::Stop => stop::execute().await,
        RecorderCommands::Toggle => toggle::execute().await,
        RecorderCommands::Pause => pause::execute().await,
        RecorderCommands::Resume => resume::execute().await,
        RecorderCommands::Status => status::execute().await,
    }
}
