//! Bar module and dropdown plumbing shared by the bar module crates.

pub mod compositor;
pub mod dropdown_registry;
pub mod icons;
pub mod module_registry;

use wayle_config::schemas::styling::Size;

/// Resolves a dropdown width/height override to a pixel request.
///
/// `None` keeps the built-in default (`base * scale`). A [`Size::Scale`]
/// multiplies the base before scaling; a [`Size::Px`] is an absolute length
/// that ignores the scale.
pub fn resolve_dimension(override_: Option<Size>, base: f32, scale: f32) -> i32 {
    match override_ {
        Some(size) => size.resolve_px(base, scale).round() as i32,
        None => (base * scale).round() as i32,
    }
}

/// Resolves an optional height override for dropdowns that otherwise size their
/// height to content.
///
/// Returns `-1` (GTK's "natural size" request) when no override applies. Only
/// an absolute [`Size::Px`] takes effect, since there is no base height to
/// scale a multiplier against.
pub fn resolve_content_height(override_: Option<Size>) -> i32 {
    match override_ {
        Some(Size::Px(px)) => px.round() as i32,
        Some(Size::Scale(_)) | None => -1,
    }
}

/// Hook the shell installs so the power bar module can open the power menu
/// window, which stays in wayle-shell.
static POWER_MENU_TRIGGER: std::sync::OnceLock<fn()> = std::sync::OnceLock::new();

/// Installs the power-menu opener. Called once by wayle-shell at startup;
/// later calls are ignored.
pub fn set_power_menu_trigger(trigger: fn()) {
    let _ = POWER_MENU_TRIGGER.set(trigger);
}

/// Opens the power menu, or does nothing if the shell has not installed the
/// trigger yet.
pub fn show_power_menu() {
    if let Some(trigger) = POWER_MENU_TRIGGER.get() {
        trigger();
    }
}
