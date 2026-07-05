use wayle_config::schemas::{
    modules::PowerProfilesConfig,
    styling::{ColorValue, ThresholdColors},
};
use wayle_power_profiles::types::profile::PowerProfile;

/// Icon for the active profile, from the per-profile config fields.
pub fn select_icon(config: &PowerProfilesConfig, profile: PowerProfile) -> String {
    match profile {
        PowerProfile::PowerSaver => config.icon_power_saver.get(),
        PowerProfile::Balanced | PowerProfile::Unknown => config.icon_balanced.get(),
        PowerProfile::Performance => config.icon_performance.get(),
    }
}

/// Per-profile color override (icon + label), from the `color-*` config fields.
pub fn select_colors(config: &PowerProfilesConfig, profile: PowerProfile) -> ThresholdColors {
    let color: ColorValue = match profile {
        PowerProfile::PowerSaver => config.color_power_saver.get(),
        PowerProfile::Balanced | PowerProfile::Unknown => config.color_balanced.get(),
        PowerProfile::Performance => config.color_performance.get(),
    };

    ThresholdColors {
        icon_color: Some(color.clone()),
        label_color: Some(color),
        ..ThresholdColors::default()
    }
}

/// Render the label format, substituting `{{ profile }}`.
pub fn format_label(format: &str, profile: PowerProfile) -> String {
    format.replace("{{ profile }}", &profile.to_string())
}

/// Next profile in the cycle, restricted to `available` when non-empty.
///
/// Falls back to the canonical power-saver → balanced → performance order.
pub fn next_profile(current: PowerProfile, available: &[PowerProfile]) -> PowerProfile {
    const ORDER: [PowerProfile; 3] = [
        PowerProfile::PowerSaver,
        PowerProfile::Balanced,
        PowerProfile::Performance,
    ];

    let cycle: Vec<PowerProfile> = if available.is_empty() {
        ORDER.to_vec()
    } else {
        ORDER
            .iter()
            .copied()
            .filter(|p| available.contains(p))
            .collect()
    };

    if cycle.is_empty() {
        return PowerProfile::Balanced;
    }

    let idx = cycle.iter().position(|&p| p == current).unwrap_or(0);
    cycle[(idx + 1) % cycle.len()]
}
