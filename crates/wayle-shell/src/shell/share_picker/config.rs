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
// Per-field base sizes (in rem) a `Size::Scale` multiplier resolves against,
// shared with the settings editor so its scale↔px conversion matches.
use wayle_config::schemas::share_picker::{
    HEIGHT_BASE_REM, OUTPUTS_SPACING_BASE_REM, WIDGET_BASE_REM, WIDTH_BASE_REM,
    WINDOWS_SPACING_BASE_REM,
};

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
}

impl PickerConfig {
    /// Resolves a snapshot from the live `[share-picker]` config section.
    ///
    /// `Size` fields resolve against the global styling scale and each field's
    /// rem base, so a `Scale(n)` is `n x base_rem x 16px x scale` (`1.0` =
    /// default), while a `Px(n)` pins an absolute pixel size.
    pub(super) fn from_config(config: &Config) -> Self {
        let sp = &config.share_picker;
        let scale = config.styling.scale.get().value();
        Self {
            width: sp.width.get().resolve_rem(WIDTH_BASE_REM, scale) as i32,
            height: sp.height.get().resolve_rem(HEIGHT_BASE_REM, scale) as i32,
            resize_size: sp.resize_size.get(),
            widget_size: sp.widget_size.get().resolve_rem(WIDGET_BASE_REM, scale) as i32,
            default_page: sp.default_page.get(),
            hide_token_restore: sp.hide_token_restore.get(),
            windows_spacing: sp
                .windows_spacing
                .get()
                .resolve_rem(WINDOWS_SPACING_BASE_REM, scale) as u32,
            windows_min_per_row: sp.windows_min_per_row.get(),
            windows_max_per_row: sp.windows_max_per_row.get(),
            outputs_spacing: sp
                .outputs_spacing
                .get()
                .resolve_rem(OUTPUTS_SPACING_BASE_REM, scale) as u32,
            outputs_show_label: sp.outputs_show_label.get(),
            outputs_respect_scaling: sp.outputs_respect_scaling.get(),
        }
    }
}
