use super::proxy::{connect, format_error};
use crate::cli::CliAction;

pub async fn execute() -> CliAction {
    let (_connection, proxy) = connect().await?;

    proxy
        .stop()
        .await
        .map_err(|e| format_error("stop recording", e))?;

    println!("Recording stopped");

    Ok(())
}
