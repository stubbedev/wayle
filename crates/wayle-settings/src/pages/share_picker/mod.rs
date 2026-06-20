//! Share picker settings page: window sizing, page defaults, per-page card
//! layout, and the enter/exit animation override.

use wayle_config::{
    Config,
    schemas::share_picker::{
        HEIGHT_BASE_REM, OUTPUTS_SPACING_BASE_REM, WIDGET_BASE_REM, WIDTH_BASE_REM,
        WINDOWS_SPACING_BASE_REM,
    },
};

use crate::{
    editors::{
        enum_select::enum_select, number::number_u32, size::size_with_base,
        surface_animation::surface_animation_rows, toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let sp = &config.share_picker;

    LeafEntry {
        id: "share-picker",
        i18n_key: "settings-nav-share-picker",
        icon: "ld-app-window-symbolic",
        spec: page_spec(
            "settings-page-share-picker",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        enum_select(&sp.default_page),
                        toggle(&sp.hide_token_restore),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-display",
                    items: vec![
                        size_with_base(&sp.width, WIDTH_BASE_REM),
                        size_with_base(&sp.height, HEIGHT_BASE_REM),
                        number_u32(&sp.resize_size),
                        size_with_base(&sp.widget_size, WIDGET_BASE_REM),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-share-picker-windows",
                    items: vec![
                        size_with_base(&sp.windows_spacing, WINDOWS_SPACING_BASE_REM),
                        number_u32(&sp.windows_min_per_row),
                        number_u32(&sp.windows_max_per_row),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-share-picker-outputs",
                    items: vec![
                        size_with_base(&sp.outputs_spacing, OUTPUTS_SPACING_BASE_REM),
                        toggle(&sp.outputs_show_label),
                        toggle(&sp.outputs_respect_scaling),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-animation",
                    items: surface_animation_rows(&config.animations.share_picker),
                },
            ],
        ),
    }
}
