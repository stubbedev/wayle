use super::proxy::{connect, format_error};
use crate::cli::CliAction;

pub async fn execute() -> CliAction {
    let (_connection, proxy) = connect().await?;

    proxy
        .resume()
        .await
        .map_err(|e| format_error("resume recording", e))?;

    println!("Resumed");

    Ok(())
}
