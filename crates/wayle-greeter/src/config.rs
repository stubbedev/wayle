//! Greeter startup options and theme configuration.
//!
//! The greeter deliberately reuses the full wayle [`Config`] so its theme,
//! background, and clock match the user's desktop and lock screen exactly. It
//! loads that config from a system path (no `$HOME` is available pre-login),
//! defaulting to `/etc/wayle/config.toml`. The remaining options are where to
//! discover Wayland sessions (`--sessions`), where to remember the last
//! session/username (`--state`), and an optional explicit fallback session
//! (`-- <argv>`).

use std::path::{Path, PathBuf};

use tracing::warn;
use wayle_config::{ApplyConfigLayer, CommitConfigReload, Config};

/// Default system config path read when `--config` is not given.
const DEFAULT_CONFIG: &str = "/etc/wayle/config.toml";

/// Directories searched for `*.desktop` session files when no `--sessions` is
/// given — the same locations sddm/gdm read (highest precedence first).
const DEFAULT_SESSION_DIRS: &[&str] = &[
    "/usr/local/share/wayland-sessions",
    "/usr/share/wayland-sessions",
];

/// Directories searched for X11 session files when no `--xsessions` is given.
const DEFAULT_XSESSION_DIRS: &[&str] = &["/usr/local/share/xsessions", "/usr/share/xsessions"];

/// Fallback state-file path used when neither `--state`, `$XDG_STATE_HOME`, nor
/// `$HOME` yields a writable location. The greetd user needs write access here.
const DEFAULT_STATE_PATH: &str = "/var/lib/wayle-greeter/last-session";

/// Parsed command-line options.
pub struct Options {
    /// Path to the wayle config used for theming (defaults to
    /// `/etc/wayle/config.toml`).
    pub config_path: PathBuf,
    /// Directories scanned for `*.desktop` Wayland session files.
    pub session_dirs: Vec<PathBuf>,
    /// Directories scanned for `*.desktop` X11 session files (launched via
    /// `startx`).
    pub xsession_dirs: Vec<PathBuf>,
    /// File the last-selected session id is remembered in. The last username
    /// is remembered in a `last-user` file next to it.
    pub state_path: PathBuf,
    /// Explicit fallback session argv from `-- <argv>`. Optional now that the
    /// greeter discovers sessions from `session_dirs`; used as an extra entry
    /// (or the only one when no `.desktop` sessions are found).
    pub command: Vec<String>,
    /// Extra `KEY=value` environment entries for the session.
    pub env: Vec<String>,
}

impl Options {
    /// Parses options from the process arguments.
    ///
    /// Usage: `wayle-greeter [--config PATH] [--sessions DIR]...
    /// [--xsessions DIR]... [--state PATH] [--env KEY=VAL]...
    /// [-- <session argv...>]`
    /// Everything after `--` is an optional explicit fallback session.
    ///
    /// # Errors
    /// Returns a usage message if required arguments are missing or malformed.
    pub fn from_args() -> Result<Self, String> {
        Self::parse(std::env::args().skip(1))
    }

    /// Parses options from an argument iterator (testable).
    fn parse(mut args: impl Iterator<Item = String>) -> Result<Self, String> {
        let mut config_path = PathBuf::from(DEFAULT_CONFIG);
        let mut session_dirs: Vec<PathBuf> = Vec::new();
        let mut xsession_dirs: Vec<PathBuf> = Vec::new();
        let mut state_path: Option<PathBuf> = None;
        let mut env = Vec::new();
        let mut command = Vec::new();
        let mut rest_is_command = false;

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
                "--sessions" => {
                    // Repeatable: each flag appends one dir, overriding the
                    // defaults once any is given.
                    let dir = args
                        .next()
                        .ok_or_else(|| usage("--sessions requires a DIR"))?;
                    session_dirs.push(PathBuf::from(dir));
                }
                "--xsessions" => {
                    let dir = args
                        .next()
                        .ok_or_else(|| usage("--xsessions requires a DIR"))?;
                    xsession_dirs.push(PathBuf::from(dir));
                }
                "--state" => {
                    state_path = Some(
                        args.next()
                            .map(PathBuf::from)
                            .ok_or_else(|| usage("--state requires a path"))?,
                    );
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

        if session_dirs.is_empty() {
            session_dirs = DEFAULT_SESSION_DIRS.iter().map(PathBuf::from).collect();
        }
        if xsession_dirs.is_empty() {
            xsession_dirs = DEFAULT_XSESSION_DIRS.iter().map(PathBuf::from).collect();
        }

        Ok(Self {
            config_path,
            session_dirs,
            xsession_dirs,
            state_path: state_path.unwrap_or_else(default_state_path),
            command,
            env,
        })
    }
}

/// Default state-file path: `$XDG_STATE_HOME/wayle-greeter/last-session`, else
/// `$HOME/.local/state/wayle-greeter/last-session`, else [`DEFAULT_STATE_PATH`].
/// greetd runs the greeter as an unprivileged user whose `$HOME` is typically
/// `/var/lib/greetd`, so an explicit `--state` in the greetd command is the
/// robust choice; this only picks a sensible default.
fn default_state_path() -> PathBuf {
    if let Some(dir) = std::env::var_os("XDG_STATE_HOME").filter(|v| !v.is_empty()) {
        return PathBuf::from(dir).join("wayle-greeter/last-session");
    }
    if let Some(home) = std::env::var_os("HOME").filter(|v| !v.is_empty()) {
        return PathBuf::from(home).join(".local/state/wayle-greeter/last-session");
    }
    PathBuf::from(DEFAULT_STATE_PATH)
}

/// Builds the usage error string.
fn usage(detail: &str) -> String {
    format!(
        "{detail}\nusage: wayle-greeter [--config PATH] [--sessions DIR]... [--xsessions DIR]... \
         [--state PATH] [--env KEY=VAL]... [-- <session argv...>]"
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
    fn no_command_is_allowed_and_sessions_default() {
        // The greeter discovers sessions itself, so `--` is optional.
        let opts = Options::parse(args(&["--config", "/tmp/c.toml"])).expect("parse");
        assert!(opts.command.is_empty());
        assert_eq!(opts.session_dirs.len(), DEFAULT_SESSION_DIRS.len());
    }

    #[test]
    fn sessions_flag_overrides_defaults_and_repeats() {
        let opts = Options::parse(args(&["--sessions", "/a", "--sessions", "/b"])).expect("parse");
        assert_eq!(
            opts.session_dirs,
            vec![PathBuf::from("/a"), PathBuf::from("/b")]
        );
    }

    #[test]
    fn xsessions_flag_overrides_defaults() {
        let opts = Options::parse(args(&["--xsessions", "/x"])).expect("parse");
        assert_eq!(opts.xsession_dirs, vec![PathBuf::from("/x")]);
        // Wayland session dirs keep their own defaults.
        assert_eq!(opts.session_dirs.len(), DEFAULT_SESSION_DIRS.len());
    }

    #[test]
    fn state_flag_is_honoured() {
        let opts = Options::parse(args(&["--state", "/run/greeter/last"])).expect("parse");
        assert_eq!(opts.state_path, PathBuf::from("/run/greeter/last"));
    }
}
