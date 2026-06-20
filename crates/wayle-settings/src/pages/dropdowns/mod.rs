//! Dropdowns settings page: per-dropdown panel size (width/height) overrides.

use wayle_config::{Config, ConfigProperty, schemas::dropdowns::DropdownSize};

use crate::{
    editors::{dropdown_size::dropdown_size_rows, surface_animation::surface_animation_rows},
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
            vec![
                size_section(&dropdowns.audio),
                size_section(&dropdowns.battery),
                size_section(&dropdowns.bluetooth),
                size_section(&dropdowns.brightness),
                size_section(&dropdowns.calendar),
                size_section(&dropdowns.dashboard),
                size_section(&dropdowns.media),
                size_section(&dropdowns.network),
                size_section(&dropdowns.notification),
                size_section(&dropdowns.weather),
                SectionSpec {
                    title_key: "settings-section-animation",
                    items: surface_animation_rows(&config.animations.dropdown),
                },
            ],
        ),
    }
}

/// One section per dropdown: the dropdown's name as the title, width + height
/// rows inside — the same shape as the general settings sections.
fn size_section(property: &ConfigProperty<DropdownSize>) -> SectionSpec {
    SectionSpec {
        title_key: property.i18n_key().unwrap_or("settings-section-sizes"),
        items: dropdown_size_rows(property),
    }
}
