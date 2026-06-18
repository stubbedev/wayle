//! Niri workspaces module settings.

use wayle_config::Config;

use crate::{
    editors::{
        color_value::color_value,
        enum_select::enum_select,
        string_list::string_list,
        string_map::string_map,
        text::{text, text_like},
        toggle::toggle,
        workspace_style_map::workspace_style_map,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

#[allow(clippy::too_many_lines)]
pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.niri_workspaces;

    LeafEntry {
        id: "niri-workspaces",
        i18n_key: "settings-nav-niri-workspaces",
        icon: "ld-grid-2x2-symbolic",
        spec: page_spec(
            "settings-page-niri-workspaces",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        toggle(&module.monitor_specific),
                        toggle(&module.hide_trailing_empty),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-display",
                    items: vec![
                        enum_select(&module.display_mode),
                        enum_select(&module.label_strategy),
                        text(&module.divider),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-app-icons",
                    items: vec![
                        toggle(&module.app_icons_show),
                        toggle(&module.app_icons_dedupe),
                        text(&module.app_icons_fallback),
                        text(&module.app_icons_empty),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-sizing",
                    items: vec![
                        text_like(&module.icon_gap),
                        text_like(&module.workspace_padding),
                        text_like(&module.icon_size),
                        text_like(&module.label_size),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-urgent",
                    items: vec![
                        toggle(&module.urgent_show),
                        enum_select(&module.urgent_mode),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-mappings",
                    items: vec![
                        workspace_style_map(&module.workspace_map),
                        string_map(&module.app_icon_map),
                        string_list(&module.workspace_ignore),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-bar-display",
                    items: vec![toggle(&module.border_show)],
                },
                SectionSpec {
                    title_key: "settings-section-colors",
                    items: vec![
                        enum_select(&module.active_indicator),
                        color_value(&module.active_color),
                        color_value(&module.occupied_color),
                        color_value(&module.empty_color),
                        color_value(&module.container_bg_color),
                        color_value(&module.border_color),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-actions",
                    items: vec![
                        text_like(&module.left_click),
                        text_like(&module.middle_click),
                        text_like(&module.right_click),
                        text_like(&module.scroll_up),
                        text_like(&module.scroll_down),
                    ],
                },
            ],
        ),
    }
}
