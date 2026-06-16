use wayle_brightness::types::BacklightType;

use crate::i18n::t;

/// Derives a human-friendly display name from a raw sysfs backlight identifier
/// (e.g. `intel_backlight` -> "Built-in display").
pub(crate) fn friendly_device_name(raw: &str, kind: BacklightType) -> String {
    let lower = raw.to_ascii_lowercase();

    if lower.contains("kbd") || lower.contains("keyboard") {
        return t!("dropdown-brightness-device-keyboard");
    }

    if lower.starts_with("ddcci") || lower.contains("ddc") || lower.contains("i2c") {
        return t!("dropdown-brightness-device-external");
    }

    if lower.contains("backlight")
        || lower.starts_with("intel_")
        || lower.starts_with("amdgpu")
        || lower.starts_with("acpi_video")
        || lower.starts_with("nvidia")
        || matches!(kind, BacklightType::Firmware | BacklightType::Platform)
    {
        return t!("dropdown-brightness-device-internal");
    }

    prettify(raw)
}

/// Title-cases a raw identifier as a fallback display name.
fn prettify(raw: &str) -> String {
    raw.split(['_', '-', ' '])
        .filter(|word| !word.is_empty())
        .map(|word| {
            let mut chars = word.chars();
            match chars.next() {
                Some(first) => first.to_uppercase().chain(chars).collect::<String>(),
                None => String::new(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

pub(crate) fn device_subtitle(
    device_name: &str,
    kind: BacklightType,
    multi: bool,
) -> Option<String> {
    if !multi {
        return None;
    }

    Some(format!(
        "{device_name} \u{00b7} {}",
        backlight_type_label(kind)
    ))
}

pub(crate) fn backlight_type_label(kind: BacklightType) -> &'static str {
    match kind {
        BacklightType::Raw => "raw",
        BacklightType::Platform => "platform",
        BacklightType::Firmware => "firmware",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn single_device_has_no_subtitle() {
        assert_eq!(
            device_subtitle("intel_backlight", BacklightType::Firmware, false),
            None
        );
    }

    #[test]
    fn multi_device_subtitle_includes_name_and_type() {
        assert_eq!(
            device_subtitle("intel_backlight", BacklightType::Firmware, true),
            Some(String::from("intel_backlight \u{00b7} firmware"))
        );
    }

    #[test]
    fn backlight_type_labels() {
        assert_eq!(backlight_type_label(BacklightType::Raw), "raw");
        assert_eq!(backlight_type_label(BacklightType::Platform), "platform");
        assert_eq!(backlight_type_label(BacklightType::Firmware), "firmware");
    }
}
