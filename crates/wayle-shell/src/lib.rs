//! Wayle desktop shell - a GTK4/Relm4 status bar for Wayland compositors.

use std::error::Error;

use relm4::RelmApp;
use tokio::runtime::Runtime;
use tracing::info;

mod bootstrap;
mod cursor_record;
mod services;
mod shell;
mod startup;
mod tracing_init;
mod wallpaper_map;
mod watchers;

// Moved to wayle-shell-core in the bar-crate split; re-exported here so the
// old `crate::…` paths keep working throughout this crate.
use shell::{Shell, ShellInit};
pub(crate) use wayle_shell_core::{i18n, notify, process};

/// Launches the Wayle shell GUI.
///
/// Creates its own tokio runtime internally, so this must not be called
/// from within an existing tokio context (it will panic).
///
/// # Errors
///
/// Returns error on tracing init failure, runtime creation failure,
/// or service bootstrap failure.
pub fn run() -> Result<(), Box<dyn Error>> {
    // No GSK_RENDERER pin: let GSK negotiate the best GPU renderer (vulkan/ngl)
    // and fall back to the cairo software renderer when no GPU path works.
    // Forcing a renderer is what *disables* that fallback chain. Override with
    // GSK_RENDERER=… to force a specific one.

    tracing_init::init()?;
    info!("Wayle shell starting");

    // Stash the session's real cursor so the pre-login greeter can match it.
    cursor_record::record();

    let runtime = Runtime::new()?;
    let _guard = runtime.enter();

    if runtime.block_on(bootstrap::is_already_running()) {
        eprintln!("Wayle shell is already running");
        return Ok(());
    }

    let (timer, services) = runtime.block_on(bootstrap::init_services())?;
    info!("Services initialized");

    let app = RelmApp::new("com.wayle.shell")
        .visible_on_activate(false)
        .with_args(vec![]);

    app.run::<Shell>(ShellInit { timer, services });

    info!("Wayle shell stopped");

    runtime.shutdown_background();
    Ok(())
}
