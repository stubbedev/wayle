//! Screenshot module settings.

use wayle_config::Config;

use crate::{
    editors::{icon::icon, text::text, toggle::toggle},
    pages::{
        nav::LeafEntry,
        sections::bar_button::{
            BarButtonFields, actions_section, bar_display_section, colors_section,
        },
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.screenshot;

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
        id: "screenshot",
        i18n_key: "settings-nav-screenshot",
        icon: "ld-camera-symbolic",
        spec: page_spec(
            "settings-page-screenshot",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        text(&module.output_directory),
                        text(&module.filename_format),
                        text(&module.label),
                        toggle(&module.copy_to_clipboard),
                        toggle(&module.notify),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-icons",
                    items: vec![icon(&module.icon)],
                },
                bar_display_section(&fields),
                colors_section(&fields),
                actions_section(
                    &fields,
                    &crate::pages::sections::action_choices::choices_for("screenshot"),
                ),
            ],
        ),
    }
}
