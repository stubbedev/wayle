use super::proxy::{connect, format_error};
use crate::cli::CliAction;

pub async fn execute() -> CliAction {
    let (_connection, proxy) = connect().await?;

    proxy
        .toggle()
        .await
        .map_err(|e| format_error("toggle recording", e))?;

    let active = proxy.active().await.unwrap_or(false);
    println!(
        "{}",
        if active {
            "Recording started"
        } else {
            "Recording stopped"
        }
    );

    Ok(())
}
