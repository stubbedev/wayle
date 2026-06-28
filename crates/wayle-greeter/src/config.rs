//! Greeter startup options and theme configuration.
//!
//! The greeter deliberately reuses the full wayle [`Config`] so its theme,
//! background, and clock match the user's desktop and lock screen exactly. It
//! loads that config from a system path (no `$HOME` is available pre-login),
//! defaulting to `/etc/wayle/config.toml`. The only greeter-specific input is
//! the session command to launch on a successful login, taken from the command
//! line.

use std::path::{Path, PathBuf};

use tracing::warn;
use wayle_config::{ApplyConfigLayer, CommitConfigReload, Config};

/// Default system config path read when `--config` is not given.
const DEFAULT_CONFIG: &str = "/etc/wayle/config.toml";

/// Parsed command-line options.
pub struct Options {
    /// Path to the wayle config used for theming (defaults to
    /// `/etc/wayle/config.toml`).
    pub config_path: PathBuf,
    /// Session argv started on successful login.
    pub command: Vec<String>,
    /// Extra `KEY=value` environment entries for the session.
    pub env: Vec<String>,
}

impl Options {
    /// Parses options from the process arguments.
    ///
    /// Usage: `wayle-greeter [--config PATH] [--env KEY=VAL]... -- <session argv...>`
    /// Everything after `--` is the session command.
    ///
    /// # Errors
    /// Returns a usage message if required arguments are missing or malformed.
    pub fn from_args() -> Result<Self, String> {
        Self::parse(std::env::args().skip(1))
    }

    /// Parses options from an argument iterator (testable).
    fn parse(args: impl Iterator<Item = String>) -> Result<Self, String> {
        let mut config_path = PathBuf::from(DEFAULT_CONFIG);
        let mut env = Vec::new();
        let mut command = Vec::new();
        let mut rest_is_command = false;
        let mut args = args.peekable();

        while let Some(arg) = args.next() {
            if rest_is_command {
                command.push(arg);
                continue;
            }
            match arg.as_str() {
                "--" => rest_is_command = true,
                "--config" => {
                    config_path = args
                        .next()
                        .map(PathBuf::from)
                        .ok_or_else(|| usage("--config requires a path"))?;
                }
                "--env" => {
                    let entry = args.next().ok_or_else(|| usage("--env requires KEY=VAL"))?;
                    if !entry.contains('=') {
                        return Err(usage("--env value must be KEY=VAL"));
                    }
                    env.push(entry);
                }
                other => return Err(usage(&format!("unexpected argument: {other}"))),
            }
        }

        if command.is_empty() {
            return Err(usage("no session command given (expected `-- <argv...>`)"));
        }

        Ok(Self {
            config_path,
            command,
            env,
        })
    }
}

/// Builds the usage error string.
fn usage(detail: &str) -> String {
    format!(
        "{detail}\nusage: wayle-greeter [--config PATH] [--env KEY=VAL]... -- <session argv...>"
    )
}

/// Loads the wayle config for theming, applying the file at `path` over the
/// built-in defaults. A missing or invalid file logs a warning and falls back
/// to defaults, so the greeter always renders.
#[must_use]
pub fn load(path: &Path) -> Config {
    let config = Config::default();
    match Config::load_toml_with_imports(path) {
        Ok(toml) => config.apply_config_layer(&toml, ""),
        Err(err) => {
            warn!(path = %path.display(), error = %err, "greeter: config load failed; using defaults")
        }
    }
    config.commit_config_reload();
    config
}

#[cfg(test)]
mod tests {
    use super::*;

    fn args(items: &[&str]) -> impl Iterator<Item = String> {
        items
            .iter()
            .map(|s| (*s).to_owned())
            .collect::<Vec<_>>()
            .into_iter()
    }

    #[test]
    fn parses_command_after_double_dash() {
        let opts = Options::parse(args(&["--", "niri", "--session"])).expect("parse");
        assert_eq!(opts.command, vec!["niri", "--session"]);
        assert_eq!(opts.config_path, PathBuf::from(DEFAULT_CONFIG));
    }

    #[test]
    fn parses_config_and_env() {
        let opts = Options::parse(args(&[
            "--config",
            "/tmp/c.toml",
            "--env",
            "XDG_SESSION_TYPE=wayland",
            "--",
            "sway",
        ]))
        .expect("parse");
        assert_eq!(opts.config_path, PathBuf::from("/tmp/c.toml"));
        assert_eq!(opts.env, vec!["XDG_SESSION_TYPE=wayland"]);
        assert_eq!(opts.command, vec!["sway"]);
    }

    #[test]
    fn missing_command_is_an_error() {
        assert!(Options::parse(args(&["--config", "/tmp/c.toml"])).is_err());
    }
}
