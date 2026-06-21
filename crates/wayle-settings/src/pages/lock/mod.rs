//! Lock screen settings page: background, clock, and security options.

use wayle_config::Config;

use crate::{
    editors::{
        color::color, enum_select::enum_select, file_picker::file_path, number::number_u32,
        surface_animation::surface_animation_rows, text::text, toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let lock = &config.lock;

    LeafEntry {
        id: "lock",
        i18n_key: "settings-nav-lock-page",
        icon: "ld-lock-symbolic",
        spec: page_spec(
            "settings-page-lock",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![toggle(&lock.enabled)],
                },
                SectionSpec {
                    title_key: "settings-section-background",
                    items: vec![
                        enum_select(&lock.background_mode),
                        file_path(&lock.background_image),
                        color(&lock.background_color),
                        number_u32(&lock.blur),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-clock",
                    items: vec![
                        toggle(&lock.show_clock),
                        text(&lock.clock_format),
                        text(&lock.date_format),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-security",
                    items: vec![
                        number_u32(&lock.grace_period_ms),
                        number_u32(&lock.max_attempts),
                        toggle(&lock.show_failed_attempts),
                        number_u32(&lock.blank_timeout_ms),
                        text(&lock.pam_service),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-animation",
                    items: surface_animation_rows(&config.animations.lock),
                },
            ],
        ),
    }
}
