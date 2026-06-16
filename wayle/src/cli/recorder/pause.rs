use super::proxy::{connect, format_error};
use crate::cli::CliAction;

pub async fn execute() -> CliAction {
    let (_connection, proxy) = connect().await?;

    proxy
        .pause()
        .await
        .map_err(|e| format_error("pause recording", e))?;

    println!("Paused");

    Ok(())
}
