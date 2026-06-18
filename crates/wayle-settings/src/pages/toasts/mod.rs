//! Toasts settings page: display options and reusable presets for `wayle toast`.

use wayle_config::Config;

use crate::{
    editors::{
        enum_select::enum_select, number::number_u32, size::size, text::text_like,
        toast_preset_list::toast_preset_list, toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let toasts = &config.toasts;

    let mut presets_editor = toast_preset_list(&toasts.presets);
    presets_editor.i18n_key = Some("settings-toasts-presets-editor");

    LeafEntry {
        id: "toasts",
        i18n_key: "settings-nav-toasts",
        icon: "ld-message-circle-symbolic",
        spec: page_spec(
            "settings-page-toasts",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![toggle(&toasts.enabled)],
                },
                SectionSpec {
                    title_key: "settings-section-display",
                    items: vec![
                        enum_select(&toasts.position),
                        enum_select(&toasts.text_align),
                        number_u32(&toasts.duration),
                        text_like(&toasts.monitor),
                        size(&toasts.margin),
                        enum_select(&toasts.layer),
                        toggle(&toasts.border),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-presets",
                    items: vec![presets_editor],
                },
            ],
        ),
    }
}
