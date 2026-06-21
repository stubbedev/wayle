//! Wayle CLI entry point.
//!
//! Parses CLI args and dispatches to the appropriate handler.
//! The `shell` subcommand runs the GUI directly and manages its own
//! tokio runtime. All other commands share a single runtime.

use std::process;

use clap::Parser;
use tokio::runtime::Runtime;
use wayle::{
    cli::{self, Cli, Commands},
    core::{init, tracing as tracing_init},
};

fn main() {
    let cli = Cli::parse();

    match cli.command {
        Commands::Shell => return run_shell(),
        Commands::Completions { shell } => {
            cli::app::generate_completions(shell);
            return;
        }
        // The portal reads the selection from stdout, so this path must not
        // initialize stdout tracing — run it in its own minimal runtime.
        Commands::SharePicker { allow_token } => return run_share_picker(allow_token),
        Commands::Portal => return run_portal(),
        _ => {}
    }

    let Ok(runtime) = Runtime::new() else {
        eprintln!("Failed to create tokio runtime");
        process::exit(1);
    };

    let result = runtime.block_on(async {
        if let Err(err) = tracing_init::init_cli_mode() {
            eprintln!("Failed to initialize tracing: {err}");
        }

        if let Err(err) = init::ensure_directories() {
            eprintln!("Failed to ensure directories: {err}");
        }

        match cli.command {
            Commands::Audio { command } => cli::audio::execute(command).await,
            Commands::Config { command } => cli::config::execute(command).await,
            Commands::Icons { command } => cli::icons::execute(command).await,
            Commands::Media { command } => cli::media::execute(command).await,
            Commands::Notify { command } => cli::notify::execute(command).await,
            Commands::Panel { command } => cli::panel::execute(command).await,
            Commands::Power { command } => cli::power::execute(command).await,
            Commands::Systray { command } => cli::systray::execute(command).await,
            Commands::Wallpaper { command } => cli::wallpaper::execute(command).await,
            Commands::Idle { command } => cli::idle::execute(command).await,
            Commands::Lock => cli::lock::execute().await,
            Commands::Recorder { command } => cli::recorder::execute(command).await,
            Commands::Screenshot { command } => cli::screenshot::execute(command).await,
            Commands::Widget { command } => cli::widget::execute(command).await,
            Commands::Toast {
                label,
                icon,
                percentage,
                duration,
                preset,
                class,
            } => {
                cli::toast::execute(
                    label.as_deref(),
                    icon.as_deref(),
                    percentage,
                    duration,
                    preset.as_deref(),
                    class.as_deref(),
                )
                .await
            }
            Commands::Shell
            | Commands::Completions { .. }
            | Commands::SharePicker { .. }
            | Commands::Portal => {
                unreachable!()
            }
        }
    });

    if let Err(err) = result {
        eprintln!("Error: {err}");
        process::exit(1);
    }
}

fn run_shell() {
    if let Err(err) = wayle_shell::run() {
        eprintln!("Error: {err}");
        process::exit(1);
    }
}

/// Runs the portal screencast picker stub in a dedicated runtime, without
/// stdout tracing, then exits with its status code.
fn run_share_picker(allow_token: bool) {
    let Ok(runtime) = Runtime::new() else {
        eprintln!("Failed to create tokio runtime");
        process::exit(1);
    };
    let code = runtime.block_on(cli::share_picker::execute(allow_token));
    process::exit(code);
}

/// Runs the xdg-desktop-portal backend in a dedicated runtime, then exits with
/// its status code. The backend blocks until terminated.
fn run_portal() {
    let Ok(runtime) = Runtime::new() else {
        eprintln!("Failed to create tokio runtime");
        process::exit(1);
    };
    if let Err(err) = tracing_init::init_cli_mode() {
        eprintln!("Failed to initialize tracing: {err}");
    }
    let code = runtime.block_on(cli::portal::execute());
    process::exit(code);
}
