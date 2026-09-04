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
            let launcher = &service.config().launcher;
            for (action, keys) in wayle_launcher::keybinds::effective(&launcher.keybindings.get()) {
                println!("kb-{action}: {keys}");
            }
            // The mouse bindings share this listing: they are the same kind
            // of thing, and `-list-keybindings` is where someone looks to
            // find out what is bound.
            let mouse = launcher.mouse_bindings.get();
            for (action, buttons) in wayle_launcher::keybinds::effective_mouse(&mouse) {
                println!("{action}: {buttons}");
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

GEOMETRY (per invocation, overriding [launcher] in config.toml):
    -width 60        percent of the monitor's width
    -width -30       width in characters
    -width 600px     width in pixels
    -lines N         visible result lines (alias of -l)
    -xoffset N       pixel offset from the anchored edge (needs -location 1-8)
    -yoffset N       pixel offset from the anchored edge (needs -location 1-8)

APPEARANCE (per invocation):
    -font 'Inter 12'   font, as a Pango description
    -style <name>      a named look from [launcher.styles] in config.toml

EVENT HOOKS (a command, run detached; {input} {entry} {mode} {error} are
substituted, one placeholder per argument, and no shell is involved):
    -on-selection-changed <cmd>   the highlighted row changed
    -on-entry-accepted <cmd>      a row (or custom input) was accepted
    -on-mode-changed <cmd>        the active mode changed
    -on-menu-canceled <cmd>       the menu was dismissed
    -on-menu-error <cmd>          the menu could not be built

THUMBNAILS:
    -preview-cmd '<cmd> {input} {output} {size}'
                     makes a row's picture instead of the system's XDG
                     thumbnailers. A row asks for one by prefixing its icon
                     with thumbnail://, e.g.
                     printf 'Name\\0icon\\x1fthumbnail:///path/to/file'
                     filebrowser and recursivebrowser ask for one per file.

MOUSE BINDINGS (-me-* buttons, -ml-* scroll; see -list-keybindings):
    -me-accept-entry MouseDPrimary     accept on a double click only
    -ml-row-down ScrollDown            wheel moves the selection

Local commands: -help, -version, -dump-config, -dump-theme, -list-keybindings";
