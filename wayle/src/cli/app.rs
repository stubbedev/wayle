use std::io;

use clap::{
    CommandFactory, Parser, Subcommand,
    builder::styling::{AnsiColor, Effects, Styles},
};
use clap_complete::Shell;

use crate::cli::{
    audio::commands::AudioCommands, config::commands::ConfigCommands,
    icons::commands::IconsCommands, idle::commands::IdleCommands, media::commands::MediaCommands,
    notify::commands::NotifyCommands, panel::commands::PanelCommands,
    power::commands::PowerCommands, recorder::commands::RecorderCommands,
    screenshot::commands::ScreenshotCommands, systray::commands::SystrayCommands,
    wallpaper::commands::WallpaperCommands, widget::commands::WidgetCommands,
};

fn get_styles() -> Styles {
    Styles::styled()
        .header(AnsiColor::Yellow.on_default().effects(Effects::BOLD))
        .usage(AnsiColor::Green.on_default().effects(Effects::BOLD))
        .literal(AnsiColor::Green.on_default().effects(Effects::BOLD))
        .placeholder(AnsiColor::Green.on_default())
        .error(AnsiColor::Red.on_default().effects(Effects::BOLD))
        .valid(AnsiColor::Green.on_default().effects(Effects::BOLD))
        .invalid(AnsiColor::Yellow.on_default().effects(Effects::BOLD))
}

/// Wayle - A Wayland compositor agnostic shell
#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
#[command(styles = get_styles())]
pub struct Cli {
    /// The command to execute.
    #[command(subcommand)]
    pub command: Commands,
}

/// Available CLI commands.
#[derive(Subcommand, Debug)]
pub enum Commands {
    /// Audio control commands
    Audio {
        /// Audio subcommand to execute.
        #[command(subcommand)]
        command: AudioCommands,
    },
    /// Configuration management commands
    Config {
        /// Configuration subcommand to execute.
        #[command(subcommand)]
        command: ConfigCommands,
    },
    /// Icon management commands
    Icons {
        /// Icons subcommand to execute.
        #[command(subcommand)]
        command: IconsCommands,
    },
    /// Media player control commands
    Media {
        /// Media subcommand to execute.
        #[command(subcommand)]
        command: MediaCommands,
    },
    /// Notification control commands
    Notify {
        /// Notification subcommand to execute.
        #[command(subcommand)]
        command: NotifyCommands,
    },
    /// Panel management commands
    Panel {
        /// Panel subcommand to execute.
        #[command(subcommand)]
        command: PanelCommands,
    },
    /// Power profile commands
    Power {
        /// Power subcommand to execute.
        #[command(subcommand)]
        command: PowerCommands,
    },
    /// System tray commands
    Systray {
        /// Systray subcommand to execute.
        #[command(subcommand)]
        command: SystrayCommands,
    },
    /// Wallpaper control commands
    Wallpaper {
        /// Wallpaper subcommand to execute.
        #[command(subcommand)]
        command: WallpaperCommands,
    },
    /// Idle inhibit control commands
    Idle {
        /// Idle subcommand to execute.
        #[command(subcommand)]
        command: IdleCommands,
    },
    /// Lock the session via Wayle's lock screen
    Lock,
    /// Screen recorder control commands
    Recorder {
        /// Recorder subcommand to execute.
        #[command(subcommand)]
        command: RecorderCommands,
    },
    /// Screenshot capture commands
    Screenshot {
        /// Screenshot subcommand to execute.
        #[command(subcommand)]
        command: ScreenshotCommands,
    },
    /// Widget control commands
    Widget {
        /// Widget subcommand to execute.
        #[command(subcommand)]
        command: WidgetCommands,
    },
    /// Show a custom on-screen toast
    Toast {
        /// Toast text. Optional when `--preset` supplies one.
        label: Option<String>,
        /// Icon name shown beside the text.
        #[arg(long)]
        icon: Option<String>,
        /// Progress percentage (0-100); shows a progress bar when set.
        #[arg(long)]
        percentage: Option<f64>,
        /// Auto-dismiss duration in milliseconds (toast config default when unset).
        #[arg(long)]
        duration: Option<u32>,
        /// Preset id from `[[toasts.presets]]` to base this toast on.
        #[arg(long)]
        preset: Option<String>,
        /// Extra CSS class applied to the toast for custom styling.
        #[arg(long)]
        class: Option<String>,
    },
    /// xdg-desktop-portal screencast picker (invoked by the portal, not by hand)
    #[command(name = "share-picker")]
    SharePicker {
        /// Pre-check the "allow restore token" box.
        #[arg(long)]
        allow_token: bool,
    },
    /// Run the desktop shell in the foreground
    Shell,
    /// Generate shell completions
    Completions {
        /// Shell to generate completions for.
        shell: Shell,
    },
}

/// Prints shell completions to stdout.
pub fn generate_completions(shell: Shell) {
    clap_complete::generate(shell, &mut Cli::command(), "wayle", &mut io::stdout());
}
