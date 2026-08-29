use crate::{
    cli::CliAction,
    config::{ConfigService, ConfigServiceCli},
};

/// Execute the command
///
/// # Errors
/// Returns error if config loading fails, value parsing fails, or path cannot be set.
pub async fn execute(path: String, value: String) -> CliAction {
    let config_service = Box::pin(ConfigService::load())
        .await
        .map_err(|e| format!("Failed to load config: {e}"))?;

    let toml_value = parse_toml_value(&value)?;

    config_service
        .set_by_path(&path, toml_value)
        .map_err(|e| format!("Failed to set config at '{path}': {e}"))?;

    config_service
        .save()
        .await
        .map_err(|e| format!("Failed to save config: {e}"))?;

    let stored = config_service
        .get_by_path(&path)
        .map_err(|e| format!("Failed to read back value: {e}"))?;

    println!("Set {path} = {stored}");

    Ok(())
}

fn parse_toml_value(value: &str) -> Result<toml::Value, String> {
    let toml_container = format!("value = {value}");

    toml::from_str::<toml::Table>(&toml_container).map_or_else(
        |_| Ok(toml::Value::String(value.to_string())),
        |mut table| {
            table
                .remove("value")
                .ok_or_else(|| String::from("Failed to parse value"))
        },
    )
}
