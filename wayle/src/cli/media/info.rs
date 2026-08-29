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

    let resolved = resolve_player(&proxy, player).await?;

    let info = proxy
        .get_player_info(resolved)
        .await
        .map_err(|e| format_error("get player info", &e))?;

    let mut output = vec![
        format!(
            "Player: {}",
            info.get("identity")
                .map_or("Unknown", String::as_str)
        ),
        format!(
            "Status: {}",
            info.get("playback_state")
                .map_or("Unknown", String::as_str)
        ),
    ];

    output.push(format!(
        "Title: {}",
        info.get("title").map_or("Unknown", String::as_str)
    ));
    output.push(format!(
        "Artist: {}",
        info.get("artist").map_or("Unknown", String::as_str)
    ));
    output.push(format!(
        "Album: {}",
        info.get("album").map_or("Unknown", String::as_str)
    ));

    if let Some(length_us) = info.get("length_us")
        && let Ok(us) = length_us.parse::<u64>()
    {
        let secs = us / 1_000_000;
        let len_mins = secs / 60;
        let len_secs = secs % 60;
        output.push(format!("Length: {len_mins:02}:{len_secs:02}"));
    }

    output.push(format!(
        "Volume: {}%",
        info.get("volume").map_or("0", String::as_str)
    ));
    output.push(format!(
        "Shuffle: {}",
        info.get("shuffle_mode")
            .map_or("Unknown", String::as_str)
    ));
    output.push(format!(
        "Loop: {}",
        info.get("loop_mode")
            .map_or("Unknown", String::as_str)
    ));

    let mut capabilities = vec![];
    if info.get("can_seek").is_some_and(|s| s == "true") {
        capabilities.push("Seek");
    }
    if info
        .get("can_go_next")
        .is_some_and(|s| s == "true")
    {
        capabilities.push("Next");
    }
    if info
        .get("can_go_previous")
        .is_some_and(|s| s == "true")
    {
        capabilities.push("Previous");
    }
    if !capabilities.is_empty() {
        output.push(format!("Capabilities: {}", capabilities.join(", ")));
    }

    println!("{}", output.join("\n"));

    Ok(())
}
