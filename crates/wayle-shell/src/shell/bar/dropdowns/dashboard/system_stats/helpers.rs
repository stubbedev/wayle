use wayle_widgets::primitives::progress_ring::ColorVariant;

/// Resolves a usage or temperature reading to a ring color given the configured
/// `warning` and `error` thresholds (both expressed in the reading's own unit).
pub(super) fn threshold_color(value: f32, warning: f32, error: f32) -> ColorVariant {
    if value >= error {
        ColorVariant::Error
    } else if value >= warning {
        ColorVariant::Warning
    } else {
        ColorVariant::Success
    }
}
