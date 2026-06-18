//! Static configuration for the share picker surface.
//!
//! Mirrors the tunables of the standalone `hyprland-preview-share-picker` but
//! ships sensible defaults baked in. CSS classes are namespaced with a
//! `share-picker-` prefix so they never collide with the bar's own styles.

/// Notebook page shown first when the picker opens.
// `Outputs`/`Region` are not selected by the baked-in default yet; they exist
// so the default page becomes configurable without reshaping this enum.
#[allow(dead_code)]
#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum Page {
    /// Per-window previews.
    Windows,
    /// Per-output previews.
    Outputs,
    /// Region selection via an external tool (e.g. `slurp`).
    Region,
}

/// Resolved picker configuration.
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

impl Default for PickerConfig {
    fn default() -> Self {
        Self {
            width: 1000,
            height: 500,
            resize_size: 640,
            widget_size: 150,
            default_page: Page::Windows,
            hide_token_restore: false,
            clicks: 2,
            windows_spacing: 12,
            windows_min_per_row: 3,
            windows_max_per_row: 4,
            outputs_spacing: 6,
            outputs_show_label: false,
            outputs_respect_scaling: true,
            region_command: String::from("slurp -f '%o@%x,%y,%w,%h'"),
        }
    }
}
