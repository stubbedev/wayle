//! Detached process spawning + terminal detection.

use std::process::Stdio;

use tracing::warn;

/// Spawn `sh -c <command>`, detached; failures are logged, not returned
/// (the surface has already closed by the time a child could fail).
pub fn run_shell(command: &str) {
    if command.trim().is_empty() {
        return;
    }
    let result = tokio::process::Command::new("sh")
        .arg("-c")
        .arg(command)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn();
    if let Err(error) = result {
        warn!(%command, %error, "launcher spawn failed");
    }
}

/// Spawn an argv directly (no shell), detached.
pub fn run_argv(argv: &[String]) {
    let Some((program, args)) = argv.split_first() else {
        return;
    };
    let result = tokio::process::Command::new(program)
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn();
    if let Err(error) = result {
        warn!(?argv, %error, "launcher spawn failed");
    }
}

const TERMINAL_FALLBACKS: &[&str] = &[
    "foot",
    "kitty",
    "alacritty",
    "wezterm",
    "ghostty",
    "gnome-terminal",
    "konsole",
    "xterm",
];

/// Resolve the terminal emulator: explicit config, `$TERMINAL`, then the
/// first fallback present in `$PATH`.
pub fn detect_terminal(configured: &str) -> String {
    if !configured.trim().is_empty() {
        return configured.trim().to_owned();
    }
    if let Ok(terminal) = std::env::var("TERMINAL")
        && !terminal.trim().is_empty()
    {
        return terminal.trim().to_owned();
    }
    TERMINAL_FALLBACKS
        .iter()
        .find(|candidate| in_path(candidate))
        .map_or_else(|| "xterm".to_owned(), |found| (*found).to_owned())
}

fn in_path(program: &str) -> bool {
    let Some(path) = std::env::var_os("PATH") else {
        return false;
    };
    std::env::split_paths(&path).any(|dir| dir.join(program).is_file())
}
