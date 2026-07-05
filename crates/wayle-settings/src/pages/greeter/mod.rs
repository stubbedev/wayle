//! Greeter (display manager) settings page: background, clock, cursor, and
//! login-form options for the `wayle-greeter` login screen.

mod apply;

use wayle_config::Config;

use crate::{
    editors::{
        color::color, enum_select::enum_select, file_picker::file_path, number::number_u32,
        text::text, toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{PageSpec, SectionSpec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let greeter = &config.greeter;

    LeafEntry {
        id: "greeter",
        i18n_key: "settings-nav-greeter-page",
        icon: "ld-monitor-symbolic",
        spec: PageSpec {
            header_key: "settings-page-greeter",
            footer: Some(apply::build_footer(config)),
            sections: vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        toggle(&greeter.show_user_list),
                        toggle(&greeter.show_power_buttons),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-background",
                    items: vec![
                        enum_select(&greeter.background_mode),
                        file_path(&greeter.background_image),
                        color(&greeter.background_color),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-clock",
                    items: vec![
                        toggle(&greeter.show_clock),
                        text(&greeter.clock_format),
                        text(&greeter.date_format),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-cursor",
                    items: vec![
                        text(&greeter.cursor_theme),
                        number_u32(&greeter.cursor_size),
                    ],
                },
            ],
        },
    }
}
