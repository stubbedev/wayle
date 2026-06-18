//! Dashboard module settings.

use wayle_config::Config;

use crate::{
    editors::{
        color_value::color_value,
        number::number_newtype,
        text::{text, text_like},
        toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, SettingRowInit, page_spec},
    },
};

/// Dashboard threshold spin (`f32` percent / °C) constrained to `[0, max]`.
fn threshold(property: &wayle_config::ConfigProperty<f32>, max: f64) -> SettingRowInit {
    number_newtype(
        property,
        0.0,
        max,
        1.0,
        0,
        |value| f64::from(*value),
        |value| value as f32,
    )
}

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.dashboard;

    LeafEntry {
        id: "dashboard",
        i18n_key: "settings-nav-dashboard",
        icon: "ld-layout-dashboard-symbolic",
        spec: page_spec(
            "settings-page-dashboard",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![text(&module.icon_override)],
                },
                SectionSpec {
                    title_key: "settings-section-commands",
                    items: vec![
                        text(&module.dropdown_lock_command),
                        text(&module.dropdown_logout_command),
                        text(&module.dropdown_reboot_command),
                        text(&module.dropdown_poweroff_command),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-thresholds",
                    items: vec![
                        threshold(&module.usage_warning, 100.0),
                        threshold(&module.usage_error, 100.0),
                        threshold(&module.temp_warning, 150.0),
                        threshold(&module.temp_error, 150.0),
                        threshold(&module.battery_warning, 100.0),
                        threshold(&module.battery_critical, 100.0),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-bar-display",
                    items: vec![toggle(&module.border_show)],
                },
                SectionSpec {
                    title_key: "settings-section-colors",
                    items: vec![
                        color_value(&module.icon_color),
                        color_value(&module.icon_bg_color),
                        color_value(&module.border_color),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-actions",
                    items: vec![
                        text_like(&module.left_click),
                        text_like(&module.right_click),
                        text_like(&module.middle_click),
                        text_like(&module.scroll_up),
                        text_like(&module.scroll_down),
                    ],
                },
            ],
        ),
    }
}
