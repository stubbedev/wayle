//! Animations settings page: global timing, indicators, and per-surface overrides.

use wayle_config::Config;

use crate::{
    editors::{
        enum_select::enum_select, number::number_u32, toggle::toggle, toml_editor::toml_editor,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let anim = &config.animations;
    let bg = &config.styling.palette.bg;

    // Per-surface overrides are stored as whole structs, so they are edited as
    // TOML rather than field-by-field controls.
    let mut notifications = toml_editor(&anim.notifications, "notifications", bg);
    notifications.i18n_key = Some("settings-animations-notifications");
    let mut osd = toml_editor(&anim.osd, "osd", bg);
    osd.i18n_key = Some("settings-animations-osd");
    let mut toast = toml_editor(&anim.toast, "toast", bg);
    toast.i18n_key = Some("settings-animations-toast");
    let mut dropdown = toml_editor(&anim.dropdown, "dropdown", bg);
    dropdown.i18n_key = Some("settings-animations-dropdown");

    LeafEntry {
        id: "animations",
        i18n_key: "settings-nav-animations",
        icon: "ld-zap-symbolic",
        spec: page_spec(
            "settings-page-animations",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        toggle(&anim.enabled),
                        enum_select(&anim.transition),
                        number_u32(&anim.duration),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-speed",
                    items: vec![
                        number_u32(&anim.ui_duration),
                        number_u32(&anim.interaction_duration),
                        toggle(&anim.indicators),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-overrides",
                    items: vec![notifications, osd, toast, dropdown],
                },
            ],
        ),
    }
}
