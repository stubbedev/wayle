//! Media module settings.

use wayle_config::Config;

use crate::{
    editors::{
        enum_select::enum_select, icon::icon, string_list::string_list, string_map::string_map,
        text::text,
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
    let module = &config.modules.media;

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
        id: "media",
        i18n_key: "settings-nav-media",
        icon: "ld-music-symbolic",
        spec: page_spec(
            "settings-page-media",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        enum_select(&module.icon_type),
                        text(&module.format),
                        icon(&module.icon_name),
                        icon(&module.spinning_disc_icon),
                        string_map(&module.player_icons),
                        string_list(&module.players_ignored),
                        string_list(&module.player_priority),
                    ],
                },
                bar_display_section(&fields),
                colors_section(&fields),
                actions_section(&fields),
            ],
        ),
    }
}
