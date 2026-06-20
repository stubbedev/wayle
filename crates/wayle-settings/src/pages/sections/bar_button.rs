//! Shared section builders for modules that use the standard bar_button pattern.

use wayle_config::{ClickAction, ConfigProperty, schemas::styling::ColorValue};

use crate::{
    editors::{
        action::{ActionChoice, action},
        color_value::color_value,
        number::number_u32,
        toggle::toggle,
    },
    pages::spec::SectionSpec,
};

pub(crate) struct BarButtonFields<'a> {
    pub(crate) icon_show: &'a ConfigProperty<bool>,
    pub(crate) label_show: &'a ConfigProperty<bool>,
    pub(crate) label_max_length: &'a ConfigProperty<u32>,
    pub(crate) border_show: &'a ConfigProperty<bool>,
    pub(crate) icon_color: &'a ConfigProperty<ColorValue>,
    pub(crate) icon_bg_color: &'a ConfigProperty<ColorValue>,
    pub(crate) label_color: &'a ConfigProperty<ColorValue>,
    pub(crate) button_bg_color: &'a ConfigProperty<ColorValue>,
    pub(crate) border_color: &'a ConfigProperty<ColorValue>,
    pub(crate) left_click: &'a ConfigProperty<ClickAction>,
    pub(crate) right_click: &'a ConfigProperty<ClickAction>,
    pub(crate) middle_click: &'a ConfigProperty<ClickAction>,
    pub(crate) scroll_up: &'a ConfigProperty<ClickAction>,
    pub(crate) scroll_down: &'a ConfigProperty<ClickAction>,
}

pub(crate) fn bar_display_section(fields: &BarButtonFields) -> SectionSpec {
    SectionSpec {
        title_key: "settings-section-bar-display",
        items: vec![
            toggle(fields.icon_show),
            toggle(fields.label_show),
            number_u32(fields.label_max_length),
            toggle(fields.border_show),
        ],
    }
}

pub(crate) fn colors_section(fields: &BarButtonFields) -> SectionSpec {
    SectionSpec {
        title_key: "settings-section-colors",
        items: vec![
            color_value(fields.icon_color),
            color_value(fields.icon_bg_color),
            color_value(fields.label_color),
            color_value(fields.button_bg_color),
            color_value(fields.border_color),
        ],
    }
}

pub(crate) fn actions_section(fields: &BarButtonFields, choices: &[ActionChoice]) -> SectionSpec {
    SectionSpec {
        title_key: "settings-section-actions",
        items: vec![
            action(fields.left_click, choices.to_vec()),
            action(fields.right_click, choices.to_vec()),
            action(fields.middle_click, choices.to_vec()),
            action(fields.scroll_up, choices.to_vec()),
            action(fields.scroll_down, choices.to_vec()),
        ],
    }
}
