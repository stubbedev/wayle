//! wayle-greeter: a greetd greeter that shares the wayle lock screen's theme.
//!
//! Runs as the single client of a kiosk compositor (e.g. `cage`) spawned by
//! `greetd`, presents the shared credential box, and drives the login over the
//! greetd IPC socket. On success greetd starts the configured session and
//! replaces this process.

mod app;
mod config;

use relm4::RelmApp;
use tracing::info;
use tracing_subscriber::EnvFilter;

fn main() {
    init_tracing();

    let options = match config::Options::from_args() {
        Ok(options) => options,
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(2);
        }
    };

    let config = config::load(&options.config_path);
    info!("wayle-greeter starting");

    let init = app::GreeterInit {
        config,
        session_cmd: options.command,
        session_env: options.env,
    };

    let app = RelmApp::new("dev.stubbe.wayle.greeter");
    app.run::<app::Greeter>(init);
}

/// Initializes tracing from `RUST_LOG` (defaulting to `info`).
fn init_tracing() {
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    tracing_subscriber::fmt().with_env_filter(filter).init();
}
