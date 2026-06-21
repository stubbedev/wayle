//! Hyprsunset module settings.

use wayle_config::Config;

use crate::{
    editors::{
        icon::icon,
        number::{number_f64, number_u32},
        text::text,
        toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        sections::bar_button::{
            BarButtonFields, actions_section, bar_display_section, colors_section,
        },
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.hyprsunset;

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
        id: "hyprsunset",
        i18n_key: "settings-nav-hyprsunset",
        icon: "ld-sun-symbolic",
        spec: page_spec(
            "settings-page-hyprsunset",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        text(&module.format),
                        number_u32(&module.temperature),
                        number_u32(&module.gamma),
                        icon(&module.icon_off),
                        icon(&module.icon_on),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-hyprsunset-schedule",
                    items: vec![
                        toggle(&module.auto_schedule),
                        number_f64(&module.latitude, -90.0, 90.0, 0.1, 4),
                        number_f64(&module.longitude, -180.0, 180.0, 0.1, 4),
                    ],
                },
                bar_display_section(&fields),
                colors_section(&fields),
                actions_section(
                    &fields,
                    &crate::pages::sections::action_choices::choices_for("hyprsunset"),
                ),
            ],
        ),
    }
}
