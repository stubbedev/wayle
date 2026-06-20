//! OSD settings page: display options for on-screen indicators.

use wayle_config::{Config, schemas::osd::MARGIN_BASE_REM};

use crate::{
    editors::{
        enum_select::enum_select, number::number_u32, size::size_with_base,
        surface_animation::surface_animation_rows, text::text_like, toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let osd = &config.osd;

    LeafEntry {
        id: "osd",
        i18n_key: "settings-nav-osd",
        icon: "ld-monitor-symbolic",
        spec: page_spec(
            "settings-page-osd",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![toggle(&osd.enabled)],
                },
                SectionSpec {
                    title_key: "settings-section-display",
                    items: vec![
                        enum_select(&osd.position),
                        enum_select(&osd.layer),
                        enum_select(&osd.text_align),
                        number_u32(&osd.duration),
                        text_like(&osd.monitor),
                        size_with_base(&osd.margin, MARGIN_BASE_REM),
                        toggle(&osd.border),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-animation",
                    items: surface_animation_rows(&config.animations.osd),
                },
            ],
        ),
    }
}
