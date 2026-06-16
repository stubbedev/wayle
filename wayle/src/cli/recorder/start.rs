use super::proxy::{connect, format_error};
use crate::cli::CliAction;

pub async fn execute() -> CliAction {
    let (_connection, proxy) = connect().await?;

    proxy
        .start()
        .await
        .map_err(|e| format_error("start recording", e))?;

    println!("Recording started");

    Ok(())
}
