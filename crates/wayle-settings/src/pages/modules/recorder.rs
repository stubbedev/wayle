//! Recorder module settings.

use wayle_config::Config;

use crate::{
    editors::{
        device_select::{microphone_device_select, webcam_device_select},
        enum_select::enum_select,
        icon::icon,
        number::number_u32_range,
        slider::{milliseconds, percentage},
        text::text,
        toggle::toggle,
    },
    pages::{
        nav::LeafEntry,
        sections::bar_button::{
            BarButtonFields, actions_section, bar_display_section, colors_section,
        },
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.recorder;

    let fields = BarButtonFields {
        icon_show: &module.icon_show,
        label_show: &module.label_show,
        label_max_length: &module.label_max_length,
        border_show: &module.border_show,
        icon_color: &module.icon_color,
        icon_bg_color: &module.icon_bg_color,
        label_color: &module.label_color,
        button_bg_color: &module.button_bg_color,
        border_color: &module.border_color,
        left_click: &module.left_click,
        right_click: &module.right_click,
        middle_click: &module.middle_click,
        scroll_up: &module.scroll_up,
        scroll_down: &module.scroll_down,
    };

    LeafEntry {
        id: "recorder",
        i18n_key: "settings-nav-recorder",
        icon: "ld-video-symbolic",
        spec: page_spec(
            "settings-page-recorder",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        text(&module.format),
                        enum_select(&module.output_format),
                        text(&module.output_directory),
                        toggle(&module.show_cursor),
                        milliseconds(&module.start_delay_ms, 0, 5000),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-video",
                    items: vec![
                        // Quality and the encoder are chosen automatically
                        // (hardware-accelerated when available, constant-quality
                        // either way), so framerate is the only video knob left.
                        number_u32_range(&module.framerate, 1, 240, 1),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-audio",
                    items: vec![
                        toggle(&module.system_audio),
                        toggle(&module.microphone),
                        microphone_device_select(&module.microphone_device),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-webcam",
                    items: vec![
                        toggle(&module.webcam_enabled),
                        webcam_device_select(&module.webcam_device),
                        percentage(&module.webcam_x),
                        percentage(&module.webcam_y),
                        percentage(&module.webcam_size),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-icons",
                    items: vec![
                        icon(&module.icon_idle),
                        icon(&module.icon_recording),
                        icon(&module.icon_paused),
                    ],
                },
                bar_display_section(&fields),
                colors_section(&fields),
                actions_section(
                    &fields,
                    &crate::pages::sections::action_choices::choices_for("recorder"),
                ),
            ],
        ),
    }
}
