//! Wallpaper settings page: single image, transitions, cycling, per-monitor.

use wayle_config::{
    Config,
    schemas::wallpaper::{CyclingInterval, TransitionDuration},
};

use crate::{
    editors::{
        enum_select::enum_select, file_picker::file_path, monitor_wallpaper::monitor_wallpaper,
        number::number_newtype, toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let wp = &config.wallpaper;

    LeafEntry {
        id: "wallpaper",
        i18n_key: "settings-nav-wallpaper",
        icon: "ld-image-symbolic",
        spec: page_spec(
            "settings-page-wallpaper",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![file_path(&wp.wallpaper)],
                },
                SectionSpec {
                    title_key: "settings-section-transition",
                    items: vec![
                        enum_select(&wp.transition),
                        number_newtype(
                            &wp.transition_duration,
                            0.0,
                            30.0,
                            0.1,
                            1,
                            |v: &TransitionDuration| v.value() as f64,
                            |seconds| TransitionDuration::new(seconds as f32),
                        ),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-cycling",
                    items: vec![
                        toggle(&wp.cycling_enabled),
                        file_path(&wp.cycling_directory),
                        enum_select(&wp.cycling_mode),
                        number_newtype(
                            &wp.cycling_interval_mins,
                            1.0,
                            1440.0,
                            1.0,
                            0,
                            |v: &CyclingInterval| v.value() as f64,
                            |seconds| CyclingInterval::new(seconds as u64),
                        ),
                        toggle(&wp.cycling_same_image),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-display",
                    items: vec![monitor_wallpaper(&wp.monitors)],
                },
            ],
        ),
    }
}
