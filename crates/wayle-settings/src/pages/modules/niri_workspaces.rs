//! Niri workspaces module settings.

use wayle_config::{
    Config,
    schemas::modules::niri_workspaces::{ICON_BASE_REM, LABEL_BASE_REM},
};

use crate::{
    editors::{
        action::action,
        color_value::color_value,
        enum_select::enum_select,
        icon::icon,
        size::{size, size_with_base},
        string_list::string_list,
        string_map::string_map,
        text::text,
        toggle::toggle,
        workspace_style_map::workspace_style_map,
    },
    pages::{
        nav::LeafEntry,
        sections::action_choices::workspace_choices,
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
                        icon(&module.app_icons_fallback),
                        icon(&module.app_icons_empty),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-sizing",
                    items: vec![
                        size(&module.icon_gap),
                        size(&module.workspace_padding),
                        size_with_base(&module.icon_size, ICON_BASE_REM),
                        size_with_base(&module.label_size, LABEL_BASE_REM),
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
                        action(&module.left_click, workspace_choices()),
                        action(&module.middle_click, workspace_choices()),
                        action(&module.right_click, workspace_choices()),
                        action(&module.scroll_up, workspace_choices()),
                        action(&module.scroll_down, workspace_choices()),
                    ],
                },
            ],
        ),
    }
}
