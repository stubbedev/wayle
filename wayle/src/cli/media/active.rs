use super::{
    proxy::{connect, format_error},
    resolve::resolve_player,
};
use crate::cli::CliAction;

/// Execute the command
///
/// # Errors
/// Returns error if D-Bus communication fails or player is not found.
pub async fn execute(player: Option<String>) -> CliAction {
    let (_connection, proxy) = connect().await?;

    if let Some(ref player_input) = player {
        let resolved = resolve_player(&proxy, Some(player_input.clone())).await?;

        proxy
            .set_active_player(resolved.clone())
            .await
            .map_err(|e| format_error("set active player", &e))?;

        println!("Set active player to: {resolved}");
        return Ok(());
    }

    let active = proxy
        .get_active_player()
        .await
        .map_err(|e| format_error("get active player", &e))?;

    if active.is_empty() {
        println!("No active player set");
        return Ok(());
    }

    let info = proxy
        .get_player_info(active)
        .await
        .map_err(|e| format_error("get player info", &e))?;

    let name = info
        .get("identity")
        .map_or("Unknown", String::as_str);
    let status = info
        .get("playback_state")
        .map_or("Unknown", String::as_str);
    let track = info.get("title").map_or("Unknown", String::as_str);

    println!("Active player: {name} - {track} [{status}]");

    Ok(())
}
