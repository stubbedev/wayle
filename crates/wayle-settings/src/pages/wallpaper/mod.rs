//! Wallpaper settings page: source image, scaling, cycling, per-monitor, animation.

use wayle_config::Config;

use crate::{
    editors::{
        enum_select::enum_select, file_picker::file_path, monitor_wallpaper::monitor_wallpaper,
        surface_animation::surface_animation_rows, wallpaper_cycling::cycling_reveal,
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
                // Source: a single image + how it's scaled.
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![file_path(&wp.wallpaper), enum_select(&wp.fit_mode)],
                },
                // Cycling: set a directory to enable; options reveal when it's set.
                SectionSpec {
                    title_key: "settings-section-cycling",
                    items: vec![
                        file_path(&wp.cycling_directory),
                        cycling_reveal(
                            &wp.cycling_directory,
                            &wp.cycling_mode,
                            &wp.cycling_interval_mins,
                            &wp.cycling_same_image,
                        ),
                    ],
                },
                // Per-monitor overrides (wallpaper + fit mode per output).
                SectionSpec {
                    title_key: "settings-section-display",
                    items: vec![monitor_wallpaper(&wp.monitors)],
                },
                // Change animation, shared with every other surface.
                SectionSpec {
                    title_key: "settings-section-animation",
                    items: surface_animation_rows(&config.animations.wallpaper),
                },
            ],
        ),
    }
}
