//! Power module settings.

use wayle_config::Config;

use crate::{
    editors::{color_value::color_value, icon::icon, text::text, toggle::toggle},
    pages::{
        nav::LeafEntry,
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn entry(config: &Config) -> LeafEntry {
    let module = &config.modules.power;

    LeafEntry {
        id: "power",
        i18n_key: "settings-nav-power",
        icon: "ld-power-symbolic",
        spec: page_spec(
            "settings-page-power",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![icon(&module.icon_name)],
                },
                SectionSpec {
                    title_key: "settings-section-bar-display",
                    items: vec![toggle(&module.border_show)],
                },
                SectionSpec {
                    title_key: "settings-section-colors",
                    items: vec![
                        color_value(&module.icon_color),
                        color_value(&module.icon_bg_color),
                        color_value(&module.border_color),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-power-menu",
                    items: vec![
                        toggle(&module.show_lock),
                        text(&module.lock_command),
                        toggle(&module.show_logout),
                        text(&module.logout_command),
                        toggle(&module.show_suspend),
                        text(&module.suspend_command),
                        toggle(&module.show_reboot),
                        text(&module.reboot_command),
                        toggle(&module.show_shutdown),
                        text(&module.shutdown_command),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-actions",
                    items: vec![
                        crate::editors::action::action(
                            &module.left_click,
                            crate::pages::sections::action_choices::choices_for("power"),
                        ),
                        crate::editors::action::action(
                            &module.right_click,
                            crate::pages::sections::action_choices::choices_for("power"),
                        ),
                        crate::editors::action::action(
                            &module.middle_click,
                            crate::pages::sections::action_choices::choices_for("power"),
                        ),
                        crate::editors::action::action(
                            &module.scroll_up,
                            crate::pages::sections::action_choices::choices_for("power"),
                        ),
                        crate::editors::action::action(
                            &module.scroll_down,
                            crate::pages::sections::action_choices::choices_for("power"),
                        ),
                    ],
                },
            ],
        ),
    }
}
