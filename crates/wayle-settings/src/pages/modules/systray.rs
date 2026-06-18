//! System tray module settings.

use wayle_config::Config;

use crate::{
    editors::{
        color_value::color_value, size::size, string_list::string_list, toggle::toggle,
        tray_override_list::tray_override_list,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.systray;

    LeafEntry {
        id: "systray",
        i18n_key: "settings-nav-systray",
        icon: "ld-panel-top-symbolic",
        spec: page_spec(
            "settings-page-systray",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        size(&module.icon_scale),
                        size(&module.item_gap),
                        size(&module.internal_padding),
                        string_list(&module.blacklist),
                        tray_override_list(&module.overrides),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-bar-display",
                    items: vec![toggle(&module.border_show)],
                },
                SectionSpec {
                    title_key: "settings-section-colors",
                    items: vec![
                        color_value(&module.border_color),
                        color_value(&module.button_bg_color),
                    ],
                },
            ],
        ),
    }
}
