use clap::Subcommand;

/// Screenshot capture subcommands.
#[derive(Subcommand, Debug)]
pub enum ScreenshotCommands {
    /// Capture a drag-selected region
    Region,
    /// Capture a whole output (the focused output when NAME is omitted)
    Output {
        /// Output connector name (e.g. DP-1).
        name: Option<String>,
    },
    /// Capture the active window
    Window,
}
