//! `wayle launcher` — rofi-compatible launcher CLI.
//!
//! Accepts rofi's flag surface (`-show drun`, `-dmenu -p pick`, ...) so
//! existing scripts work via `alias rofi='wayle launcher'` or a `rofi`
//! symlink to the wayle binary. Sessions run in the shell daemon; this
//! command talks to it over the launcher socket and exits with rofi's
//! codes (0 accept, 1 cancel, 10-28 kb-custom-N).

pub mod args;
mod client;

use wayle_config::{ConfigService, ConfigServiceCli};

use self::args::LocalCmd;
use crate::cli::CliAction;

/// Execute with raw rofi-style args. Exits the process for non-zero codes.
///
/// # Errors
///
/// Returns usage errors from flag parsing.
pub async fn execute(arguments: Vec<String>) -> CliAction {
    let invocation = args::parse(&arguments)?;

    if let Some(local) = invocation.local {
        return run_local(local).await;
    }
    if invocation.options.mode.is_none()
        && !invocation.options.dmenu
        && invocation.options.error_message.is_none()
        && invocation.options.modes.is_none()
    {
        return Err(String::from(
            "nothing to show: pass -show <mode>, -dmenu, or -e <message>",
        ));
    }

    let code = client::run(invocation).await;
    if code != 0 {
        std::process::exit(code);
    }
    Ok(())
}

async fn run_local(local: LocalCmd) -> CliAction {
    match local {
        LocalCmd::Help => {
            println!("{HELP}");
            Ok(())
        }
        LocalCmd::Version => {
            println!("wayle launcher {}", env!("CARGO_PKG_VERSION"));
            Ok(())
        }
        LocalCmd::DumpConfig => {
            let service = ConfigService::load()
                .await
                .map_err(|error| format!("failed to load config: {error}"))?;
            let value = service
                .get_by_path("launcher")
                .map_err(|error| format!("failed to read [launcher]: {error}"))?;
            println!(
                "{}",
                toml::to_string_pretty(&value).map_err(|error| error.to_string())?
            );
            Ok(())
        }
        LocalCmd::DumpTheme => {
            println!(
                "# wayle does not use rasi themes; the launcher is styled by the\n\
                 # wayle palette/SCSS system. See `wayle-settings` (Launcher page)\n\
                 # and the [styling] config section."
            );
            Ok(())
        }
        LocalCmd::ListKeybindings => {
            let service = ConfigService::load()
                .await
                .map_err(|error| format!("failed to load config: {error}"))?;
            let overrides = service.config().launcher.keybindings.get();
            for (action, keys) in wayle_launcher::keybinds::effective(&overrides) {
                println!("kb-{action}: {keys}");
            }
            Ok(())
        }
    }
}

const HELP: &str = "wayle launcher — rofi-compatible application launcher / dmenu

USAGE:
    wayle launcher -show <mode>       open a mode (drun, run, window, ssh, ...)
    wayle launcher -dmenu [...]       dmenu mode: rows from stdin, selection to stdout
    wayle launcher -e <message>       message dialog

Accepts the common rofi option surface (-p, -mesg, -multi-select, -matching,
-location, -drun-*, -window-*, -kb-*, ...). rasi theming options (-theme,
-theme-str) are accepted but ignored — style via wayle-settings instead.
Exit codes match rofi: 0 accept, 1 cancel, 10-28 for kb-custom-N.

Local commands: -help, -version, -dump-config, -dump-theme, -list-keybindings";
