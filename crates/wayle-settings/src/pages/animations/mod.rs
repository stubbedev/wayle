//! Animations settings page: global timing, indicators, and per-surface overrides.

use wayle_config::Config;

use crate::{
    editors::{
        enum_select::enum_select,
        number::number_u32,
        optional::{enum_select_optional, number_u32_optional},
        surface_animation::surface_animation,
        toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

/// Upper bound (ms) for the optional enter/exit duration spins.
const MAX_DURATION_MS: u32 = 100_000;
/// Step (ms) for duration spins.
const DURATION_STEP_MS: u32 = 10;
/// Spin value shown when a duration leaves the inherited state.
const DURATION_FALLBACK_MS: u32 = 200;

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let anim = &config.animations;

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
                    title_key: "settings-section-direction",
                    items: vec![
                        enum_select_optional(&anim.enter),
                        enum_select_optional(&anim.exit),
                        number_u32_optional(
                            &anim.enter_duration,
                            0,
                            MAX_DURATION_MS,
                            DURATION_STEP_MS,
                            DURATION_FALLBACK_MS,
                        ),
                        number_u32_optional(
                            &anim.exit_duration,
                            0,
                            MAX_DURATION_MS,
                            DURATION_STEP_MS,
                            DURATION_FALLBACK_MS,
                        ),
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
                    items: vec![
                        surface_animation(&anim.notifications, "settings-animations-notifications"),
                        surface_animation(&anim.osd, "settings-animations-osd"),
                        surface_animation(&anim.toast, "settings-animations-toast"),
                        surface_animation(&anim.dropdown, "settings-animations-dropdown"),
                    ],
                },
            ],
        ),
    }
}
