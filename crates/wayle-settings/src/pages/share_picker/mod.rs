//! Share picker settings page: window sizing, page defaults, per-page card
//! layout, and the enter/exit animation override.

use wayle_config::Config;

use crate::{
    editors::{
        enum_select::enum_select, number::number_u32, surface_animation::surface_animation_rows,
        text::text_like, toggle::toggle,
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
                        number_u32(&sp.clicks),
                        toggle(&sp.hide_token_restore),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-display",
                    items: vec![
                        number_u32(&sp.width),
                        number_u32(&sp.height),
                        number_u32(&sp.resize_size),
                        number_u32(&sp.widget_size),
                        text_like(&sp.region_command),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-share-picker-windows",
                    items: vec![
                        number_u32(&sp.windows_spacing),
                        number_u32(&sp.windows_min_per_row),
                        number_u32(&sp.windows_max_per_row),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-share-picker-outputs",
                    items: vec![
                        number_u32(&sp.outputs_spacing),
                        toggle(&sp.outputs_show_label),
                        toggle(&sp.outputs_respect_scaling),
                    ],
                },
                SectionSpec {
                    title_key: "settings-animations-share-picker",
                    items: surface_animation_rows(&config.animations.share_picker),
                },
            ],
        ),
    }
}
