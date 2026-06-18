//! Power profiles module settings.

use wayle_config::Config;

use crate::{
    editors::{color_value::color_value, icon::icon, text::text},
    pages::{
        nav::LeafEntry,
        sections::bar_button::{
            BarButtonFields, actions_section, bar_display_section, colors_section,
        },
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.power_profiles;

    let fields = BarButtonFields {
        icon_show: &module.icon_show,
        label_show: &module.label_show,
        label_max_length: &module.label_max_length,
        border_show: &module.border_show,
        icon_color: &module.icon_color,
        icon_bg_color: &module.icon_bg_color,
        label_color: &module.label_color,
        button_bg_color: &module.button_bg_color,
        border_color: &module.border_color,
        left_click: &module.left_click,
        right_click: &module.right_click,
        middle_click: &module.middle_click,
        scroll_up: &module.scroll_up,
        scroll_down: &module.scroll_down,
    };

    LeafEntry {
        id: "power-profiles",
        i18n_key: "settings-nav-power-profiles",
        icon: "ld-scale-symbolic",
        spec: page_spec(
            "settings-page-power-profiles",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![text(&module.format)],
                },
                SectionSpec {
                    title_key: "settings-section-power-profiles-profiles",
                    items: vec![
                        icon(&module.icon_power_saver),
                        color_value(&module.color_power_saver),
                        icon(&module.icon_balanced),
                        color_value(&module.color_balanced),
                        icon(&module.icon_performance),
                        color_value(&module.color_performance),
                    ],
                },
                bar_display_section(&fields),
                colors_section(&fields),
                actions_section(&fields),
            ],
        ),
    }
}
