//! Per-session resolution: merge CLI [`SessionOptions`] over the
//! `[launcher]` config into engine configs, UI settings, and mode instances.

use std::collections::BTreeMap;

use tracing::warn;
use wayle_config::{
    Config,
    schemas::launcher::{
        LauncherCase, LauncherDrunField, LauncherLocation, LauncherMatching, LauncherSorting,
        WIDTH_BASE_REM,
    },
};
use wayle_ipc::launcher_socket::SessionOptions;
use wayle_launcher::{
    CaseMode, MatchMethod, MatcherOptions, Mode, SortMethod,
    history::HistoryStore,
    modes::{DrunConfig, DrunField, DrunMode, RunConfig, RunMode},
};

/// Resolved UI knobs for one session.
pub(super) struct UiSettings {
    /// Surface width in pixels.
    pub width: i32,
    /// Visible result lines.
    pub lines: u32,
    /// Keep list height fixed at `lines`.
    pub fixed_num_lines: bool,
    /// Surface position.
    pub location: LauncherLocation,
    /// Hide typed input (`-password`).
    pub password: bool,
    /// Message row (rofi `-mesg`).
    pub mesg: Option<String>,
    /// Message-dialog mode (`-e`): show only this text.
    pub error_message: Option<String>,
    /// Pre-filled query (`-filter`).
    pub filter: Option<String>,
    /// Prompt override (`-p`); modes supply their own otherwise.
    pub prompt: Option<String>,
    /// Show row icons.
    pub show_icons: bool,
    /// Show mode tabs.
    pub sidebar: bool,
    /// Per-mode display names.
    pub display_names: BTreeMap<String, String>,
    /// Effective keybindings (defaults ← config ← `-kb-*`).
    pub keybindings: Vec<(String, String)>,
}

/// Everything the surface needs to run one session.
pub(super) struct SessionSetup {
    /// Engine modes, in tab order.
    pub modes: Vec<Box<dyn Mode>>,
    /// Index of the initially shown mode.
    pub initial_mode: usize,
    /// Matching options.
    pub matcher: MatcherOptions,
    /// UI knobs.
    pub ui: UiSettings,
}

/// Resolve a session from CLI options merged over the live config.
pub(super) fn build(options: &SessionOptions, config: &Config) -> SessionSetup {
    let launcher = &config.launcher;
    let scale = config.styling.scale.get().value();

    let history = launcher
        .history
        .enable
        .get()
        .then(|| match HistoryStore::open() {
            Ok(store) => Some(store),
            Err(error) => {
                warn!(%error, "launcher history unavailable");
                None
            }
        })
        .flatten();
    let max_history = launcher.history.max_size.get();

    let mode_names = requested_modes(options, launcher);
    let mut modes: Vec<Box<dyn Mode>> = Vec::new();
    for name in &mode_names {
        match build_mode(name, options, config, history.clone(), max_history) {
            Some(mode) => modes.push(mode),
            None => warn!(mode = %name, "launcher mode not available; skipped"),
        }
    }
    let initial_mode = options
        .mode
        .as_ref()
        .and_then(|wanted| modes.iter().position(|mode| mode.name() == wanted))
        .unwrap_or(0);

    let mut keybindings = launcher.keybindings.get();
    for (action, keys) in &options.kb_overrides {
        keybindings.insert(action.clone(), keys.clone());
    }
    let mut display_names = launcher.display_names.get();
    for (mode, name) in &options.display_names {
        display_names.insert(mode.clone(), name.clone());
    }

    SessionSetup {
        modes,
        initial_mode,
        matcher: matcher_options(options, config),
        ui: UiSettings {
            width: launcher.width.get().resolve_rem(WIDTH_BASE_REM, scale) as i32,
            lines: options.lines.unwrap_or_else(|| launcher.lines.get()),
            fixed_num_lines: !options.no_fixed_num_lines && launcher.fixed_num_lines.get(),
            location: options
                .location
                .and_then(location_from_rofi)
                .unwrap_or_else(|| launcher.location.get()),
            password: options.password,
            mesg: options.mesg.clone(),
            error_message: options.error_message.clone(),
            filter: options.filter.clone(),
            prompt: options.prompt.clone(),
            show_icons: options.show_icons.unwrap_or_else(|| launcher.show_icons.get()),
            sidebar: options
                .sidebar_mode
                .unwrap_or_else(|| launcher.sidebar_mode.get()),
            display_names,
            keybindings: wayle_launcher::keybinds::effective(&keybindings),
        },
    }
}

/// The mode list for this session: `-modes`, else `[launcher].modes`, with
/// `-show <mode>` guaranteed present.
fn requested_modes(
    options: &SessionOptions,
    launcher: &wayle_config::schemas::launcher::LauncherConfig,
) -> Vec<String> {
    let mut names = options.modes.clone().unwrap_or_else(|| launcher.modes.get());
    if let Some(mode) = &options.mode
        && !names.contains(mode)
    {
        names.insert(0, mode.clone());
    }
    names
}

fn build_mode(
    name: &str,
    options: &SessionOptions,
    config: &Config,
    history: Option<HistoryStore>,
    max_history: u32,
) -> Option<Box<dyn Mode>> {
    match name {
        "drun" => Some(Box::new(DrunMode::new(
            drun_config(options, config, max_history),
            history,
        ))),
        "run" => Some(Box::new(RunMode::new(
            run_config(options, config, max_history),
            history,
        ))),
        // window/ssh/filebrowser/keys/combi/scripts land in later phases.
        _ => None,
    }
}

fn terminal(options: &SessionOptions, config: &Config) -> String {
    options
        .terminal
        .clone()
        .unwrap_or_else(|| config.launcher.terminal.get())
}

fn drun_config(options: &SessionOptions, config: &Config, max_history: u32) -> DrunConfig {
    let drun = &config.launcher.drun;
    let match_fields = options.drun_match_fields.as_ref().map_or_else(
        || drun.match_fields.get().iter().map(drun_field).collect(),
        |fields| {
            if fields.iter().any(|raw| raw == "all") {
                vec![
                    DrunField::Name,
                    DrunField::Generic,
                    DrunField::Exec,
                    DrunField::Categories,
                    DrunField::Comment,
                    DrunField::Keywords,
                ]
            } else {
                fields.iter().filter_map(|raw| drun_field_str(raw)).collect()
            }
        },
    );
    DrunConfig {
        categories: options
            .drun_categories
            .clone()
            .unwrap_or_else(|| drun.categories.get()),
        exclude_categories: options
            .drun_exclude_categories
            .clone()
            .unwrap_or_else(|| drun.exclude_categories.get()),
        match_fields,
        display_format: options
            .drun_display_format
            .clone()
            .unwrap_or_else(|| drun.display_format.get()),
        show_actions: options
            .drun_show_actions
            .unwrap_or_else(|| drun.show_actions.get()),
        url_launcher: options
            .drun_url_launcher
            .clone()
            .unwrap_or_else(|| drun.url_launcher.get()),
        terminal: terminal(options, config),
        max_history,
    }
}

fn run_config(options: &SessionOptions, config: &Config, max_history: u32) -> RunConfig {
    let run = &config.launcher.run;
    RunConfig {
        run_command: options
            .run_command
            .clone()
            .unwrap_or_else(|| run.run_command.get()),
        shell_command: options
            .run_shell_command
            .clone()
            .unwrap_or_else(|| run.shell_command.get()),
        list_command: options
            .run_list_command
            .clone()
            .unwrap_or_else(|| run.list_command.get()),
        terminal: terminal(options, config),
        max_history,
    }
}

fn matcher_options(options: &SessionOptions, config: &Config) -> MatcherOptions {
    let launcher = &config.launcher;
    let method = options.matching.as_deref().map_or_else(
        || match_method(launcher.matching.get()),
        |raw| match raw {
            "regex" => MatchMethod::Regex,
            "glob" => MatchMethod::Glob,
            "fuzzy" => MatchMethod::Fuzzy,
            "prefix" => MatchMethod::Prefix,
            _ => MatchMethod::Normal,
        },
    );
    let case = if options.case_insensitive == Some(true) {
        CaseMode::Insensitive
    } else if options.case_smart == Some(true) {
        CaseMode::Smart
    } else if options.case_sensitive == Some(true) {
        CaseMode::Sensitive
    } else {
        match launcher.case.get() {
            LauncherCase::Insensitive => CaseMode::Insensitive,
            LauncherCase::Smart => CaseMode::Smart,
            LauncherCase::Sensitive => CaseMode::Sensitive,
        }
    };
    let sort_method = options.sorting_method.as_deref().map_or_else(
        || match launcher.sorting_method.get() {
            LauncherSorting::Levenshtein => SortMethod::Levenshtein,
            LauncherSorting::Fzf => SortMethod::Fzf,
        },
        |raw| match raw {
            "fzf" | "fzf-v2" => SortMethod::Fzf,
            _ => SortMethod::Levenshtein,
        },
    );
    MatcherOptions {
        method,
        case,
        tokenize: options.tokenize.unwrap_or_else(|| launcher.tokenize.get()),
        normalize: options
            .normalize_match
            .unwrap_or_else(|| launcher.normalize_match.get()),
        negation_char: options
            .negate_char
            .or_else(|| launcher.negate_char.get().chars().next())
            .unwrap_or('-'),
        sort: options.sort.unwrap_or_else(|| launcher.sort.get()),
        sort_method,
    }
}

fn match_method(matching: LauncherMatching) -> MatchMethod {
    match matching {
        LauncherMatching::Normal => MatchMethod::Normal,
        LauncherMatching::Regex => MatchMethod::Regex,
        LauncherMatching::Glob => MatchMethod::Glob,
        LauncherMatching::Fuzzy => MatchMethod::Fuzzy,
        LauncherMatching::Prefix => MatchMethod::Prefix,
    }
}

fn drun_field(field: &LauncherDrunField) -> DrunField {
    match field {
        LauncherDrunField::Name => DrunField::Name,
        LauncherDrunField::Generic => DrunField::Generic,
        LauncherDrunField::Exec => DrunField::Exec,
        LauncherDrunField::Categories => DrunField::Categories,
        LauncherDrunField::Comment => DrunField::Comment,
        LauncherDrunField::Keywords => DrunField::Keywords,
    }
}

fn drun_field_str(raw: &str) -> Option<DrunField> {
    match raw {
        "name" => Some(DrunField::Name),
        "generic" => Some(DrunField::Generic),
        "exec" => Some(DrunField::Exec),
        "categories" => Some(DrunField::Categories),
        "comment" => Some(DrunField::Comment),
        "keywords" => Some(DrunField::Keywords),
        _ => None, // "all" expanded by the caller
    }
}

/// rofi numeric `-location` 0-8 → the location enum.
fn location_from_rofi(location: u8) -> Option<LauncherLocation> {
    Some(match location {
        0 => LauncherLocation::Center,
        1 => LauncherLocation::NorthWest,
        2 => LauncherLocation::North,
        3 => LauncherLocation::NorthEast,
        4 => LauncherLocation::East,
        5 => LauncherLocation::SouthEast,
        6 => LauncherLocation::South,
        7 => LauncherLocation::SouthWest,
        8 => LauncherLocation::West,
        _ => return None,
    })
}
