//! Wayle Settings GUI.
//!
//! Separate binary from the shell. Reads the same config files,
//! writes runtime overrides to runtime.toml, and the shell picks
//! up changes via its file watcher. Closes cleanly when the window
//! is dismissed, freeing all RAM.

mod app;
mod editors;
mod pages;
mod property_handle;
mod row;
mod sidebar;

use std::process;

use relm4::gtk::prelude::*;
use tokio::runtime::Runtime;
use tracing_subscriber::EnvFilter;
use wayle_config::{ConfigService, PersistenceWatcher};

fn main() {
    // No GSK_RENDERER pin: let GSK negotiate the best GPU renderer
    // (vulkan/ngl) and fall back to the cairo software renderer when no GPU
    // path works. Forcing a renderer is what *disables* that fallback chain.
    // Override with GSK_RENDERER=… to force a specific one.

    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    let runtime = match Runtime::new() {
        Ok(runtime) => runtime,
        Err(err) => {
            eprintln!("cannot create tokio runtime: {err}");
            process::exit(1);
        }
    };

    let _guard = runtime.enter();

    let config_service = match runtime.block_on(ConfigService::load()) {
        Ok(service) => service,
        Err(err) => {
            eprintln!("cannot load config: {err}");
            process::exit(1);
        }
    };

    let _persistence = match PersistenceWatcher::start(config_service.clone()) {
        Ok(watcher) => watcher,
        Err(err) => {
            eprintln!("cannot start persistence watcher: {err}");
            process::exit(1);
        }
    };

    let app = relm4::main_application();
    app.set_application_id(Some("com.wayle.settings"));

    // Raise the existing window when launched again; relm4 only calls
    // set_visible, which is a no-op once the window is already shown.
    app.connect_activate(|app| {
        if let Some(window) = app.active_window() {
            window.present();
        }
    });

    let relm_app = relm4::RelmApp::from_app(app);

    relm_app.run::<app::SettingsApp>(config_service);
}
