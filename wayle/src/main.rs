//! Wayle CLI entry point.
//!
//! Parses CLI args and dispatches to the appropriate handler.
//! The `shell` subcommand runs the GUI directly and manages its own
//! tokio runtime. All other commands share a single runtime.

use std::process;

use clap::Parser;
use tokio::runtime::Runtime;
use wayle::{
    cli::{self, Cli, Commands, portal::PortalCommands},
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
        // The portal backend and picker manage their own runtime and tracing,
        // so they bypass the shared runtime below.
        Commands::Portal { command } => return run_portal(command),
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
            Commands::Shell | Commands::Completions { .. } | Commands::Portal { .. } => {
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
    // Editor-support schema files (config.toml JSON schema + tombi config).
    // Lives here rather than in wayle-shell's bootstrap so only this binary
    // needs wayle-config's `schema` feature (schemars stays out of the
    // shell's build graph).
    if let Err(err) = wayle_config::infrastructure::schema::ensure_schema_current() {
        eprintln!("Warning: could not write schema file: {err}");
    }

    if let Err(err) = wayle_shell::run() {
        eprintln!("Error: {err}");
        process::exit(1);
    }
}

/// Runs a `wayle portal` subcommand in a dedicated runtime, then exits with its
/// status code.
///
/// The variants differ in tracing setup: the share-picker stub writes its
/// selection to stdout for the portal frontend to parse, so it must not
/// initialize stdout tracing; the backend (the default) and the dialog
/// previewer both do. The backend blocks until terminated.
fn run_portal(command: Option<PortalCommands>) {
    let Ok(runtime) = Runtime::new() else {
        eprintln!("Failed to create tokio runtime");
        process::exit(1);
    };

    let code = runtime.block_on(async {
        match command {
            Some(PortalCommands::SharePicker { allow_token }) => {
                cli::portal::share_picker::execute(allow_token).await
            }
            Some(PortalCommands::Show { dialog }) => {
                if let Err(err) = tracing_init::init_cli_mode() {
                    eprintln!("Failed to initialize tracing: {err}");
                }
                match cli::portal::show::execute(dialog).await {
                    Ok(()) => 0,
                    Err(err) => {
                        eprintln!("Error: {err}");
                        1
                    }
                }
            }
            None | Some(PortalCommands::Run) => {
                if let Err(err) = tracing_init::init_cli_mode() {
                    eprintln!("Failed to initialize tracing: {err}");
                }
                cli::portal::backend::execute().await
            }
        }
    });
    process::exit(code);
}
