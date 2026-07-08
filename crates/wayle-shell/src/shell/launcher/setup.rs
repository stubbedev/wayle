//! Per-session resolution: merge CLI [`SessionOptions`] over the
//! `[launcher]` config into engine configs, UI settings, and mode instances.

use std::collections::BTreeMap;

use tracing::warn;
use wayle_config::{
    Config,
    schemas::launcher::{
        LauncherCase, LauncherDrunField, LauncherFileSort, LauncherLocation, LauncherMatching,
        LauncherSorting, LauncherWindowField, WIDTH_BASE_REM,
    },
};
use wayle_ipc::launcher_socket::SessionOptions;
use wayle_launcher::{
    CaseMode, MatchMethod, MatcherOptions, Mode, SortMethod,
    history::HistoryStore,
    modes::{
        CombiMode, DmenuConfig, DmenuMode, DrunConfig, DrunField, DrunMode, FileBrowserConfig,
        FileBrowserMode, FileSort, KeysMode, RunConfig, RunMode, ScriptMode, SshConfig, SshMode,
        WindowConfig, WindowField, WindowMode,
    },
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
    /// Wrap selection at list edges.
    pub cycle: bool,
    /// Accept automatically when exactly one result remains.
    pub auto_select: bool,
    /// Pre-select the first entry matching this string (`-select`).
    pub select: Option<String>,
    /// Pre-select this row (`-selected-row`).
    pub selected_row: Option<u32>,
    /// 1-based columns of each row to display (`-display-columns`).
    pub display_columns: Option<Vec<u32>>,
    /// Column separator (`-display-column-separator`, default tab).
    pub column_separator: String,
    /// Row text truncation: "start" | "middle" | "end" (`-keep-right` =
    /// start).
    pub ellipsize: String,
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
/// `dmenu_rows` is the CLI's row stream for `-dmenu` sessions.
pub(super) fn build(
    options: &SessionOptions,
    config: &Config,
    dmenu_rows: Option<tokio::sync::mpsc::Receiver<Vec<String>>>,
) -> SessionSetup {
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

    let mut keybindings = launcher.keybindings.get();
    for (action, keys) in &options.kb_overrides {
        keybindings.insert(action.clone(), keys.clone());
    }
    let effective_bindings = wayle_launcher::keybinds::effective(&keybindings);

    let mut modes: Vec<Box<dyn Mode>> = Vec::new();
    if let Some(rows) = dmenu_rows {
        modes.push(Box::new(DmenuMode::new(dmenu_config(options), rows)));
    } else {
        let mode_names = requested_modes(options, launcher);
        for name in &mode_names {
            match build_mode(
                name,
                options,
                config,
                history.clone(),
                max_history,
                &effective_bindings,
            ) {
                Some(mode) => modes.push(mode),
                None => warn!(mode = %name, "launcher mode not available; skipped"),
            }
        }
    }
    let initial_mode = options
        .mode
        .as_ref()
        .and_then(|wanted| modes.iter().position(|mode| mode.name() == wanted))
        .unwrap_or(0);
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
            keybindings: effective_bindings,
            cycle: options.cycle.unwrap_or_else(|| launcher.cycle.get()),
            auto_select: options
                .auto_select
                .unwrap_or_else(|| launcher.auto_select.get()),
            select: options.select.clone(),
            selected_row: options.selected_row,
            display_columns: options.display_columns.clone(),
            column_separator: options
                .display_column_separator
                .clone()
                .unwrap_or_else(|| "\t".to_owned()),
            ellipsize: if options.keep_right {
                "start".to_owned()
            } else {
                options
                    .ellipsize_mode
                    .clone()
                    .unwrap_or_else(|| "end".to_owned())
            },
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
    bindings: &[(String, String)],
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
        "window" => Some(Box::new(WindowMode::new(window_config(
            options, config, false,
        )))),
        "windowcd" => Some(Box::new(WindowMode::new(window_config(
            options, config, true,
        )))),
        "ssh" => Some(Box::new(SshMode::new(
            ssh_config(options, config, max_history),
            history,
        ))),
        "filebrowser" => Some(Box::new(FileBrowserMode::new(filebrowser_config(
            config, false,
        )))),
        "recursivebrowser" => Some(Box::new(FileBrowserMode::new(filebrowser_config(
            config, true,
        )))),
        "keys" => Some(Box::new(KeysMode::new(bindings.to_vec()))),
        "combi" => {
            let combi = &config.launcher.combi;
            let children: Vec<Box<dyn Mode>> = combi
                .modes
                .get()
                .iter()
                .filter(|child| child.as_str() != "combi") // no recursion
                .filter_map(|child| {
                    build_mode(child, options, config, history.clone(), max_history, bindings)
                })
                .collect();
            if children.is_empty() {
                return None;
            }
            Some(Box::new(CombiMode::new(
                children,
                options
                    .combi_display_format
                    .clone()
                    .unwrap_or_else(|| combi.display_format.get()),
            )))
        }
        // Custom script modes: `name:script` inline, or a [launcher.scripts] key.
        other => {
            if let Some((name, script)) = other.split_once(':') {
                return Some(Box::new(ScriptMode::new(name, expand_home(script))));
            }
            config
                .launcher
                .scripts
                .get()
                .get(other)
                .map(|script| Box::new(ScriptMode::new(other, expand_home(script))) as Box<dyn Mode>)
        }
    }
}

fn expand_home(path: &str) -> String {
    match path.strip_prefix("~") {
        Some(rest) => format!("{}{rest}", std::env::var("HOME").unwrap_or_default()),
        None => path.to_owned(),
    }
}

fn dmenu_config(options: &SessionOptions) -> DmenuConfig {
    DmenuConfig {
        prompt: options.prompt.clone(),
        message: options.mesg.clone(),
        markup_rows: options.markup_rows,
        multi_select: options.multi_select,
        no_custom: options.no_custom || options.only_match,
        urgent: options.urgent.clone().unwrap_or_default(),
        active: options.active.clone().unwrap_or_default(),
    }
}

fn window_config(options: &SessionOptions, config: &Config, current_only: bool) -> WindowConfig {
    let window = &config.launcher.window;
    let match_fields = options.window_match_fields.as_ref().map_or_else(
        || {
            window
                .match_fields
                .get()
                .iter()
                .map(|field| match field {
                    LauncherWindowField::Title => WindowField::Title,
                    LauncherWindowField::Class => WindowField::Class,
                    LauncherWindowField::Name => WindowField::Name,
                    LauncherWindowField::Role => WindowField::Role,
                    LauncherWindowField::Desktop => WindowField::Desktop,
                })
                .collect()
        },
        |fields| {
            fields
                .iter()
                .flat_map(|raw| match raw.as_str() {
                    "title" => vec![WindowField::Title],
                    "class" => vec![WindowField::Class],
                    "name" => vec![WindowField::Name],
                    "role" => vec![WindowField::Role],
                    "desktop" => vec![WindowField::Desktop],
                    "all" => vec![
                        WindowField::Title,
                        WindowField::Class,
                        WindowField::Name,
                        WindowField::Role,
                        WindowField::Desktop,
                    ],
                    _ => Vec::new(),
                })
                .collect()
        },
    );
    WindowConfig {
        format: options
            .window_format
            .clone()
            .unwrap_or_else(|| window.format.get()),
        match_fields,
        hide_active: options
            .hide_active_window
            .unwrap_or_else(|| window.hide_active.get()),
        close_on_delete: window.close_on_delete.get(),
        window_command: options.window_command.clone().unwrap_or_default(),
        current_desktop_only: current_only,
    }
}

fn ssh_config(options: &SessionOptions, config: &Config, max_history: u32) -> SshConfig {
    let ssh = &config.launcher.ssh;
    SshConfig {
        client: options.ssh_client.clone().unwrap_or_else(|| ssh.client.get()),
        command: options.ssh_command.clone().unwrap_or_else(|| ssh.command.get()),
        parse_hosts: options.parse_hosts.unwrap_or_else(|| ssh.parse_hosts.get()),
        parse_known_hosts: options
            .parse_known_hosts
            .unwrap_or_else(|| ssh.parse_known_hosts.get()),
        terminal: terminal(options, config),
        max_history,
    }
}

fn filebrowser_config(config: &Config, recursive: bool) -> FileBrowserConfig {
    let browser = &config.launcher.filebrowser;
    FileBrowserConfig {
        directory: browser.directory.get(),
        sorting: match browser.sorting_method.get() {
            LauncherFileSort::Name => FileSort::Name,
            LauncherFileSort::Mtime => FileSort::Mtime,
            LauncherFileSort::Atime => FileSort::Atime,
            LauncherFileSort::Ctime => FileSort::Ctime,
        },
        directories_first: browser.directories_first.get(),
        show_hidden: browser.show_hidden.get(),
        command: browser.command.get(),
        recursive,
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
