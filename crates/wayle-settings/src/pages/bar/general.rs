//! Bar general settings: layout, appearance, spacing, and border.

use wayle_config::Config;

use crate::{
    editors::{
        bar_layout::bar_layout,
        color_value::color_value,
        enum_select::enum_select,
        number::{number_u8, scale},
        slider::percentage,
        text::text_like,
        toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let bar = &config.bar;

    LeafEntry {
        id: "bar-general",
        i18n_key: "settings-nav-bar-general",
        icon: "ld-layout-dashboard-symbolic",
        spec: page_spec(
            "settings-page-bar-general",
            vec![
                SectionSpec {
                    title_key: "settings-section-layout",
                    items: vec![
                        enum_select(&bar.location),
                        toggle(&bar.exclusive),
                        enum_select(&bar.layer),
                        scale(&bar.scale),
                        bar_layout(&bar.layout, &config.modules.custom),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-appearance",
                    items: vec![
                        color_value(&bar.bg),
                        percentage(&bar.background_opacity),
                        enum_select(&bar.rounding),
                        enum_select(&bar.shadow),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-spacing",
                    items: vec![
                        text_like(&bar.inset_edge),
                        text_like(&bar.inset_ends),
                        text_like(&bar.padding),
                        text_like(&bar.padding_ends),
                        text_like(&bar.module_gap),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-border",
                    items: vec![
                        enum_select(&bar.border_location),
                        number_u8(&bar.border_width),
                        color_value(&bar.border_color),
                    ],
                },
            ],
        ),
    }
}
