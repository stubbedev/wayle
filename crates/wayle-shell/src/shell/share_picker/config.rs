//! Resolved configuration for the share picker surface.
//!
//! [`PickerConfig`] is a plain snapshot the view builders read from. It is
//! resolved from the user's `[share-picker]` config section
//! ([`SharePickerConfig`]) each time the picker opens, so live edits in
//! wayle-settings take effect on the next request. CSS classes are namespaced
//! with a `share-picker-` prefix so they never collide with the bar's own
//! styles.

use wayle_config::Config;
pub(super) use wayle_config::schemas::share_picker::SharePickerPage as Page;

/// Resolved picker configuration snapshot.
pub(super) struct PickerConfig {
    /// Target window width in pixels.
    pub(super) width: i32,
    /// Target window height in pixels.
    pub(super) height: i32,
    /// Downscale every captured frame to at most this height.
    pub(super) resize_size: u32,
    /// Height of the card image widget.
    pub(super) widget_size: i32,
    /// Page selected on open.
    pub(super) default_page: Page,
    /// Hide the restore-token checkbox.
    pub(super) hide_token_restore: bool,
    /// Clicks required to select a window/output card.
    pub(super) clicks: u32,
    /// Spacing between window cards.
    pub(super) windows_spacing: u32,
    /// Minimum window cards per row.
    pub(super) windows_min_per_row: u32,
    /// Maximum window cards per row.
    pub(super) windows_max_per_row: u32,
    /// Spacing between output cards (applied per side).
    pub(super) outputs_spacing: u32,
    /// Show the output name label under each output card.
    pub(super) outputs_show_label: bool,
    /// Scale output cards by their fractional scale.
    pub(super) outputs_respect_scaling: bool,
    /// Region selection command, parsed with shell-word splitting.
    pub(super) region_command: String,
}

impl PickerConfig {
    /// Resolves a snapshot from the live `[share-picker]` config section.
    pub(super) fn from_config(config: &Config) -> Self {
        let sp = &config.share_picker;
        Self {
            width: sp.width.get() as i32,
            height: sp.height.get() as i32,
            resize_size: sp.resize_size.get(),
            widget_size: sp.widget_size.get() as i32,
            default_page: sp.default_page.get(),
            hide_token_restore: sp.hide_token_restore.get(),
            clicks: sp.clicks.get(),
            windows_spacing: sp.windows_spacing.get(),
            windows_min_per_row: sp.windows_min_per_row.get(),
            windows_max_per_row: sp.windows_max_per_row.get(),
            outputs_spacing: sp.outputs_spacing.get(),
            outputs_show_label: sp.outputs_show_label.get(),
            outputs_respect_scaling: sp.outputs_respect_scaling.get(),
            region_command: sp.region_command.get(),
        }
    }
}
