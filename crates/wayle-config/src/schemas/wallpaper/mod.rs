mod types;

use schemars::schema_for;
pub use types::{CyclingInterval, CyclingMode, FitMode, MonitorWallpaperConfig};
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, ModuleInfo, ModuleInfoProvider},
};

/// Wallpaper rendering, cycling, and per-monitor overrides.
#[wayle_config(i18n_prefix = "settings-wallpaper")]
pub struct WallpaperConfig {
    /// A single image file to use as the wallpaper on all monitors. Leave empty
    /// to use cycling and/or per-monitor overrides instead.
    #[serde(rename = "wallpaper")]
    #[default(String::new())]
    pub wallpaper: ConfigProperty<String>,

    /// How the wallpaper is scaled to the screen. Per-monitor entries in
    /// `[[wallpaper.monitors]]` override this.
    #[serde(rename = "fit-mode")]
    #[default(FitMode::Fill)]
    pub fit_mode: ConfigProperty<FitMode>,

    // The wallpaper change animation is configured under `[animations]`
    // (`AnimSurface::Wallpaper`), shared with every other surface.
    /// Enable automatic wallpaper cycling.
    #[serde(rename = "cycling-enabled")]
    #[default(false)]
    pub cycling_enabled: ConfigProperty<bool>,

    /// Directory containing wallpaper images for cycling.
    #[serde(rename = "cycling-directory")]
    #[default(String::new())]
    pub cycling_directory: ConfigProperty<String>,

    /// Wallpaper cycling order.
    #[serde(rename = "cycling-mode")]
    #[default(CyclingMode::Sequential)]
    pub cycling_mode: ConfigProperty<CyclingMode>,

    /// Time between wallpaper changes in minutes.
    #[serde(rename = "cycling-interval-mins")]
    #[default(CyclingInterval::DEFAULT)]
    pub cycling_interval_mins: ConfigProperty<CyclingInterval>,

    /// Show the same cycling wallpaper on all monitors. Only affects shuffle
    /// mode since sequential already displays the same image.
    #[serde(rename = "cycling-same-image")]
    #[default(false)]
    pub cycling_same_image: ConfigProperty<bool>,

    /// Per-monitor wallpaper and fit mode settings. Each entry targets a
    /// monitor by connector name. See [`MonitorWallpaperConfig`] for the
    /// available fields.
    ///
    /// ## Example
    ///
    /// ```toml
    /// [[wallpaper.monitors]]
    /// name = "DP-1"
    /// wallpaper = "/home/me/pictures/wall-primary.png"
    /// fit-mode = "fill"
    ///
    /// [[wallpaper.monitors]]
    /// name = "HDMI-1"
    /// wallpaper = "/home/me/pictures/wall-secondary.png"
    /// fit-mode = "fit"
    /// ```
    #[default(Vec::new())]
    pub monitors: ConfigProperty<Vec<MonitorWallpaperConfig>>,
}

impl ModuleInfoProvider for WallpaperConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("wallpaper"),
            schema: || schema_for!(WallpaperConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        vec![
            ConfigGroup::general(),
            ConfigGroup::prefix("Cycling", "cycling-"),
            ConfigGroup::standalone("Per-monitor overrides", "monitors"),
        ]
    }
}

crate::register_module!(WallpaperConfig);
