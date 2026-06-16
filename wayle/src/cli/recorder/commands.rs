use clap::Subcommand;

/// Screen recorder control subcommands.
#[derive(Subcommand, Debug)]
pub enum RecorderCommands {
    /// Start recording
    Start,
    /// Stop recording
    Stop,
    /// Toggle recording on/off
    Toggle,
    /// Pause the active recording
    Pause,
    /// Resume a paused recording
    Resume,
    /// Show current recorder status
    Status,
}
