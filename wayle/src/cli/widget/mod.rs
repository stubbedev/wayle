/// Widget command definitions
pub mod commands;
/// Update a widget's output
pub mod update;

use commands::WidgetCommands;

use super::CliAction;

/// Executes widget control commands.
///
/// # Errors
/// Returns an error if the shell is not running or the update is rejected.
pub async fn execute(command: WidgetCommands) -> CliAction {
    match command {
        WidgetCommands::Update { id, output } => update::execute(&id, &output).await,
    }
}
