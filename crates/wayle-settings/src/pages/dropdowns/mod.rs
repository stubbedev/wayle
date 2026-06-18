//! Dropdowns settings page: per-dropdown panel size (width/height) overrides.

use wayle_config::Config;

use crate::{
    editors::dropdown_size::dropdown_size,
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let dropdowns = &config.dropdowns;

    LeafEntry {
        id: "dropdowns",
        i18n_key: "settings-nav-dropdowns",
        icon: "ld-panel-top-open-symbolic",
        spec: page_spec(
            "settings-page-dropdowns",
            vec![SectionSpec {
                title_key: "settings-section-sizes",
                items: vec![
                    dropdown_size(&dropdowns.audio),
                    dropdown_size(&dropdowns.battery),
                    dropdown_size(&dropdowns.bluetooth),
                    dropdown_size(&dropdowns.brightness),
                    dropdown_size(&dropdowns.calendar),
                    dropdown_size(&dropdowns.dashboard),
                    dropdown_size(&dropdowns.media),
                    dropdown_size(&dropdowns.network),
                    dropdown_size(&dropdowns.notification),
                    dropdown_size(&dropdowns.weather),
                ],
            }],
        ),
    }
}
