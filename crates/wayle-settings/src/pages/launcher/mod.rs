//! Launcher (rofi replacement) settings pages.

use wayle_config::{Config, schemas::launcher::WIDTH_BASE_REM};

use crate::{
    editors::{
        enum_list::enum_list, enum_select::enum_select, number::number_u32,
        size::size_with_base, string_list::string_list, string_map::string_map,
        surface_animation::surface_animation_rows, text::text, toggle::toggle,
    },
    pages::{
        nav::{LeafEntry, PageFactory},
        spec::{SectionSpec, page_spec},
    },
};

pub(crate) fn factories() -> Vec<PageFactory> {
    vec![entry, modes_entry]
}

/// Main launcher page: surface, matching, history, keybindings.
fn entry(config: &Config) -> LeafEntry {
    let launcher = &config.launcher;

    LeafEntry {
        id: "launcher",
        i18n_key: "settings-nav-launcher-page",
        icon: "ld-rocket-symbolic",
        spec: page_spec(
            "settings-page-launcher",
            vec![
                SectionSpec {
                    title_key: "settings-section-general",
                    items: vec![
                        enum_select(&launcher.location),
                        size_with_base(&launcher.width, WIDTH_BASE_REM),
                        number_u32(&launcher.lines),
                        text(&launcher.monitor),
                        string_list(&launcher.modes),
                        toggle(&launcher.cycle),
                        toggle(&launcher.fixed_num_lines),
                        toggle(&launcher.sidebar_mode),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-matching",
                    items: vec![
                        enum_select(&launcher.matching),
                        toggle(&launcher.tokenize),
                        text(&launcher.negate_char),
                        toggle(&launcher.normalize_match),
                        toggle(&launcher.sort),
                        enum_select(&launcher.sorting_method),
                        enum_select(&launcher.case),
                        toggle(&launcher.auto_select),
                        toggle(&launcher.hover_select),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-appearance",
                    items: vec![
                        toggle(&launcher.show_icons),
                        text(&launcher.icon_theme),
                        text(&launcher.terminal),
                        string_map(&launcher.display_names),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-history",
                    items: vec![
                        toggle(&launcher.history.enable),
                        number_u32(&launcher.history.max_size),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-keybindings",
                    items: vec![string_map(&launcher.keybindings)],
                },
                SectionSpec {
                    title_key: "settings-section-animation",
                    items: surface_animation_rows(&config.animations.launcher),
                },
            ],
        ),
    }
}

/// Per-mode page: drun, run, window, ssh, filebrowser, combi, scripts.
fn modes_entry(config: &Config) -> LeafEntry {
    let launcher = &config.launcher;

    LeafEntry {
        id: "launcher-modes",
        i18n_key: "settings-nav-launcher-modes-page",
        icon: "ld-grid-2x2-symbolic",
        spec: page_spec(
            "settings-page-launcher-modes",
            vec![
                SectionSpec {
                    title_key: "settings-section-launcher-drun",
                    items: vec![
                        string_list(&launcher.drun.categories),
                        string_list(&launcher.drun.exclude_categories),
                        enum_list(&launcher.drun.match_fields),
                        text(&launcher.drun.display_format),
                        toggle(&launcher.drun.show_actions),
                        text(&launcher.drun.url_launcher),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-run",
                    items: vec![
                        text(&launcher.run.run_command),
                        text(&launcher.run.shell_command),
                        text(&launcher.run.list_command),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-window",
                    items: vec![
                        text(&launcher.window.format),
                        enum_list(&launcher.window.match_fields),
                        toggle(&launcher.window.hide_active),
                        toggle(&launcher.window.close_on_delete),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-ssh",
                    items: vec![
                        text(&launcher.ssh.client),
                        text(&launcher.ssh.command),
                        toggle(&launcher.ssh.parse_hosts),
                        toggle(&launcher.ssh.parse_known_hosts),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-filebrowser",
                    items: vec![
                        text(&launcher.filebrowser.directory),
                        enum_select(&launcher.filebrowser.sorting_method),
                        toggle(&launcher.filebrowser.directories_first),
                        toggle(&launcher.filebrowser.show_hidden),
                        text(&launcher.filebrowser.command),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-combi",
                    items: vec![
                        string_list(&launcher.combi.modes),
                        text(&launcher.combi.display_format),
                    ],
                },
                SectionSpec {
                    title_key: "settings-section-launcher-scripts",
                    items: vec![string_map(&launcher.scripts)],
                },
            ],
        ),
    }
}
