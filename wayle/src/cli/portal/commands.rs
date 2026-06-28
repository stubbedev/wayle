//! Subcommands under `wayle portal`.

use clap::Subcommand;

/// `wayle portal` subcommands: the backend daemon, the screencast picker stub,
/// and the developer dialog previewer.
#[derive(Subcommand, Debug)]
pub enum PortalCommands {
    /// Run the xdg-desktop-portal backend (the default when no subcommand is
    /// given, so the installed D-Bus/systemd units can keep execing
    /// `wayle portal`).
    Run,
    /// xdg-desktop-portal-hyprland screencast picker stub (invoked by the
    /// portal, not by hand).
    #[command(name = "share-picker")]
    SharePicker {
        /// Pre-check the "allow restore token" box.
        #[arg(long)]
        allow_token: bool,
    },
    /// Preview a portal dialog UI without an application request (developer
    /// tool; talks to the running shell over D-Bus, same as the real backend).
    Show {
        /// Which portal dialog to show.
        #[command(subcommand)]
        dialog: PortalDialog,
    },
}

/// The portal dialogs `wayle portal show` can pop, one per shell-backed
/// `org.freedesktop.impl.portal.*` interface.
#[derive(Subcommand, Debug)]
pub enum PortalDialog {
    /// File open/save dialog (`org.freedesktop.impl.portal.FileChooser`).
    #[command(name = "file-chooser")]
    FileChooser {
        /// Show the save dialog instead of the open dialog.
        #[arg(long)]
        save: bool,
        /// Allow selecting multiple files (open only).
        #[arg(long)]
        multiple: bool,
        /// Select a directory instead of files (open only).
        #[arg(long)]
        directory: bool,
    },
    /// Screenshot capture (`org.freedesktop.impl.portal.Screenshot`).
    Screenshot {
        /// Capture mode: `region`, `output`, `screen`, or `window`.
        #[arg(long, default_value = "region")]
        mode: String,
        /// Output connector name (used by `output` mode).
        #[arg(long, default_value = "")]
        target: String,
    },
    /// Interactive color picker (`Screenshot.PickColor`).
    Color,
    /// Printer selection dialog (`org.freedesktop.impl.portal.Print`); only the
    /// printer/settings prepare step is shown, no document is spooled.
    Print,
    /// Screencast source picker (`org.freedesktop.impl.portal.ScreenCast`).
    #[command(name = "screen-cast")]
    ScreenCast {
        /// Pre-check the "allow restore token" box.
        #[arg(long)]
        allow_token: bool,
        /// Allow selecting multiple sources.
        #[arg(long)]
        multiple: bool,
    },
    /// Generic grant/deny access prompt (`org.freedesktop.impl.portal.Access`).
    Access,
    /// Account info sharing consent (`org.freedesktop.impl.portal.Account`).
    Account,
    /// Application chooser (`org.freedesktop.impl.portal.AppChooser`).
    #[command(name = "app-chooser")]
    AppChooser,
    /// Dynamic launcher install confirmation
    /// (`org.freedesktop.impl.portal.DynamicLauncher`).
    #[command(name = "dynamic-launcher")]
    DynamicLauncher,
    /// Wallpaper preview confirmation
    /// (`org.freedesktop.impl.portal.Wallpaper` `show-preview`).
    Wallpaper {
        /// `file://` image URI to preview (defaults to a placeholder).
        #[arg(long, default_value = "")]
        uri: String,
    },
}
