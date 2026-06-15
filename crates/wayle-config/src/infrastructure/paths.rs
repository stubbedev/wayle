//! Re-exports path resolution from wayle-core, plus config-local path helpers.

use std::path::PathBuf;

pub use wayle_core::paths::ConfigPaths;

/// Path to `themes/schema.json` for theme file validation.
pub fn theme_schema_json() -> PathBuf {
    ConfigPaths::themes_dir().join("schema.json")
}

/// Resolves the user's main config file, preferring YAML over TOML.
///
/// Discovery order: `config.yaml`, `config.yml`, then `config.toml`. When no
/// config file exists yet, returns the `config.toml` path (which the loader
/// then creates with default contents), keeping TOML the default for fresh
/// installs while letting users opt into YAML by simply naming their file
/// `config.yaml`.
pub fn discover_main_config() -> PathBuf {
    if let Ok(dir) = ConfigPaths::config_dir() {
        for name in ["config.yaml", "config.yml"] {
            let candidate = dir.join(name);
            if candidate.exists() {
                return candidate;
            }
        }
    }
    ConfigPaths::main_config()
}
