//! Notifications settings page: popup display, positioning, and filtering.

use wayle_config::{
    Config,
    schemas::modules::notification::{POPUP_GAP_BASE_REM, POPUP_MARGIN_BASE_REM},
};

use crate::{
    editors::{
        enum_select::enum_select, number::number_u32, size::size_with_base,
        string_list::string_list, surface_animation::surface_animation_rows, text::text_like,
        toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let notif = &config.modules.notifications;

    LeafEntry {
        id: "notifications",
        i18n_key: "settings-nav-notifications",
        icon: "ld-bell-symbolic",
        spec: page_spec(
            "settings-page-notifications",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![toggle(&notif.enabled)],
                },
                SectionSpec {
                    title_key: "settings-section-popup-display",
                    items: vec![
                        enum_select(&notif.popup_position),
                        number_u32(&notif.popup_max_visible),
                        enum_select(&notif.popup_stacking_order),
                        number_u32(&notif.popup_duration),
                        toggle(&notif.popup_hover_pause),
                        toggle(&notif.popup_shadow),
                        enum_select(&notif.popup_close_behavior),
                        enum_select(&notif.popup_urgency_bar),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-positioning",
                    items: vec![
                        text_like(&notif.popup_monitor),
                        enum_select(&notif.popup_layer),
                        size_with_base(&notif.popup_margin_x, POPUP_MARGIN_BASE_REM),
                        size_with_base(&notif.popup_margin_y, POPUP_MARGIN_BASE_REM),
                        size_with_base(&notif.popup_gap, POPUP_GAP_BASE_REM),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-filtering",
                    items: vec![
                        enum_select(&notif.icon_source),
                        string_list(&notif.blocklist),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-animation",
                    items: surface_animation_rows(&config.animations.notifications),
                },
            ],
        ),
    }
}
