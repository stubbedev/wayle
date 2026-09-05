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
use wayle_ipc::launcher_socket::{LauncherWidth, SessionOptions};
use wayle_launcher::{
    CaseMode, Hooks, MatchMethod, MatcherOptions, Mode, SortMethod,
    history::HistoryStore,
    modes::{
        CalcMode, ClipboardMode, CombiMode, DmenuConfig, DmenuMode, DrunConfig, DrunField,
        DrunMode, EmojiMode, FileBrowserConfig, FileBrowserMode, FileSort, KeysMode, RunConfig,
        RunMode, ScriptMode, SshConfig, SshMode, WindowConfig, WindowField, WindowMode,
    },
};

/// Resolved UI knobs for one session.
pub(super) struct UiSettings {
    /// Surface width in pixels, from `[launcher] width`.
    pub width: i32,
    /// `-width`: per-invocation override for `width`. Resolved late (in
    /// `apply_ui`) because percent and character widths need the monitor and
    /// the font, which the config layer can't see.
    pub width_override: Option<LauncherWidth>,
    /// `-xoffset`/`-yoffset`: pixel offsets from the anchored edges.
    pub offset: (i32, i32),
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
    /// Effective mouse bindings (defaults ← config ← `-me-*`/`-ml-*`).
    pub mouse_bindings: Vec<(String, String)>,
    /// `-on-*` commands for this session.
    pub hooks: Hooks,
    /// The CSS this session adds on top of `[styling]`: the `-font`/config
    /// font and the `-style` preset, already rendered.
    pub css: Option<String>,
    /// `-preview-cmd`: how a `thumbnail://` icon becomes an image file.
    pub preview_cmd: Option<String>,
    /// `-completer-mode`: the mode `kb-mode-complete` opens.
    pub completer: Option<String>,
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
            width_override: options.width,
            offset: (
                options.xoffset.unwrap_or_default(),
                options.yoffset.unwrap_or_default(),
            ),
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
            show_icons: options
                .show_icons
                .unwrap_or_else(|| launcher.show_icons.get()),
            sidebar: options
                .sidebar_mode
                .unwrap_or_else(|| launcher.sidebar_mode.get()),
            display_names,
            keybindings: effective_bindings,
            mouse_bindings: mouse_bindings(options, launcher),
            hooks: hooks(options),
            css: session_css(options, launcher),
            preview_cmd: preview_cmd(options, launcher),
            completer: options
                .completer_mode
                .clone()
                .filter(|name| !name.is_empty()),
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
            ellipsize: ellipsize(options),
        },
    }
}

/// Row truncation: `-keep-right` is `-ellipsize-mode start` by another name.
fn ellipsize(options: &SessionOptions) -> String {
    if options.keep_right {
        "start".to_owned()
    } else {
        options
            .ellipsize_mode
            .clone()
            .unwrap_or_else(|| "end".to_owned())
    }
}

/// The mode list for this session: `-modes`, else `[launcher].modes`, with
/// `-show <mode>` guaranteed present.
fn requested_modes(
    options: &SessionOptions,
    launcher: &wayle_config::schemas::launcher::LauncherConfig,
) -> Vec<String> {
    let mut names = options
        .modes
        .clone()
        .unwrap_or_else(|| launcher.modes.get());
    if let Some(mode) = &options.mode
        && !names.contains(mode)
    {
        names.insert(0, mode.clone());
    }
    // The completer has to be one of the session's own modes for the key to
    // reach it, and `-completer-mode` naming one is the whole request — so
    // it is loaded even when `-modes` did not list it. It is appended, so it
    // is never the mode the session opens on.
    if let Some(completer) = &options.completer_mode
        && !completer.is_empty()
        && !names.contains(completer)
    {
        names.push(completer.clone());
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
        "calc" => Some(Box::new(CalcMode::new())),
        "clipboard" => Some(Box::new(ClipboardMode::new())),
        "emoji" => Some(Box::new(EmojiMode::new())),
        "combi" => {
            let combi = &config.launcher.combi;
            let children: Vec<Box<dyn Mode>> = combi
                .modes
                .get()
                .iter()
                .filter(|child| child.as_str() != "combi") // no recursion
                .filter_map(|child| {
                    build_mode(
                        child,
                        options,
                        config,
                        history.clone(),
                        max_history,
                        bindings,
                    )
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
            config.launcher.scripts.get().get(other).map(|script| {
                Box::new(ScriptMode::new(other, expand_home(script))) as Box<dyn Mode>
            })
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
        client: options
            .ssh_client
            .clone()
            .unwrap_or_else(|| ssh.client.get()),
        command: options
            .ssh_command
            .clone()
            .unwrap_or_else(|| ssh.command.get()),
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
                fields
                    .iter()
                    .filter_map(|raw| drun_field_str(raw))
                    .collect()
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
        fallback_icon: options
            .application_fallback_icon
            .clone()
            .unwrap_or_default(),
        ignored_prefixes: options.ignored_prefixes.clone().unwrap_or_default(),
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
        ignored_prefixes: options.ignored_prefixes.clone().unwrap_or_default(),
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

/// A Pango font description as GTK CSS properties.
///
/// The two syntaxes are not interchangeable and it is easy to assume they
/// are: Pango puts the size last (`"Monospace 20"`), CSS's `font:` shorthand
/// puts it first (`font: 20pt Monospace`), so handing CSS a Pango string
/// yields a declaration GTK silently drops — `-font` looked like it did
/// nothing at all. Parsing it and emitting the individual properties is what
/// makes rofi's spelling work.
fn font_properties(description: &str) -> String {
    let description = relm4::gtk::pango::FontDescription::from_string(description);
    let mut properties = Vec::new();

    let family = description.family().unwrap_or_default();
    if !family.is_empty() {
        properties.push(format!("font-family: \"{family}\";"));
    }

    // Pango sizes are in points scaled by `pango::SCALE`, unless the
    // description says they are absolute pixels.
    let size = description.size();
    if size > 0 {
        let points = f64::from(size) / f64::from(relm4::gtk::pango::SCALE);
        let unit = if description.is_size_absolute() {
            "px"
        } else {
            "pt"
        };
        properties.push(format!("font-size: {points}{unit};"));
    }

    if description.style() == relm4::gtk::pango::Style::Italic {
        properties.push(String::from("font-style: italic;"));
    }
    // CSS takes the numeric weight, which is the value pango's enum wraps.
    let weight = relm4::gtk::glib::translate::IntoGlib::into_glib(description.weight());
    if weight > 0 && description.weight() != relm4::gtk::pango::Weight::Normal {
        properties.push(format!("font-weight: {weight};"));
    }

    properties.join(" ")
}

/// The `-on-*` commands this session asked for.
fn hooks(options: &SessionOptions) -> Hooks {
    Hooks {
        selection_changed: options.on_selection_changed.clone(),
        entry_accepted: options.on_entry_accepted.clone(),
        mode_changed: options.on_mode_changed.clone(),
        menu_canceled: options.on_menu_canceled.clone(),
        menu_error: options.on_menu_error.clone(),
    }
}

/// `-preview-cmd`, else the configured one, else the system thumbnailers.
fn preview_cmd(
    options: &SessionOptions,
    launcher: &wayle_config::schemas::launcher::LauncherConfig,
) -> Option<String> {
    options
        .preview_cmd
        .clone()
        .or_else(|| Some(launcher.preview_cmd.get()))
        .filter(|command| !command.trim().is_empty())
}

/// Effective mouse bindings: defaults, then config, then `-me-*`/`-ml-*`.
fn mouse_bindings(
    options: &SessionOptions,
    launcher: &wayle_config::schemas::launcher::LauncherConfig,
) -> Vec<(String, String)> {
    let mut mouse = launcher.mouse_bindings.get();
    for (action, buttons) in &options.mouse_overrides {
        mouse.insert(action.clone(), buttons.clone());
    }
    wayle_launcher::keybinds::effective_mouse(&mouse)
}

/// The CSS one session adds on top of `[styling]`, from the font and the
/// `-style` preset — or `None` when it asked for neither.
///
/// One provider for both because they want the same lifecycle: GTK4 has no
/// per-widget style provider, so anything per-session has to be a provider
/// added to the display for the session's lifetime and removed after.
///
/// Scoped to `.launcher-surface` so a preset cannot reach the bar.
fn session_css(
    options: &SessionOptions,
    launcher: &wayle_config::schemas::launcher::LauncherConfig,
) -> Option<String> {
    let mut css = String::new();

    let font = options
        .font
        .clone()
        .unwrap_or_else(|| launcher.font.get())
        .trim()
        .to_owned();
    if !font.is_empty() {
        // `.launcher-surface`, not `.launcher-window`: the surface declares
        // its own `font-family` (`_launcher.scss`), so a rule on the window
        // above it is inherited and then immediately overridden. Same
        // specificity here means the provider priority decides, which is
        // what makes the override actually win.
        css.push_str(&format!(
            ".launcher-surface {{ {} }}\n",
            font_properties(&font)
        ));
    }

    if let Some(name) = &options.style {
        match launcher.styles.get().get(name) {
            Some(preset) => css.push_str(preset),
            // Naming a preset that does not exist is a typo in a bind, and
            // silently rendering the default look is how it stays unnoticed.
            None => warn!(style = %name, "launcher: no such [launcher.styles] preset"),
        }
    }

    (!css.trim().is_empty()).then_some(css)
}

#[cfg(test)]
mod tests {
    use wayle_config::Config;

    use super::*;

    fn launcher_config() -> Config {
        Config::default()
    }

    #[test]
    fn a_session_asking_for_neither_font_nor_style_adds_no_css() {
        let config = launcher_config();
        assert!(session_css(&SessionOptions::default(), &config.launcher).is_none());
    }

    #[test]
    fn the_font_flag_becomes_css_scoped_to_the_launcher() {
        let config = launcher_config();
        let options = SessionOptions {
            font: Some(String::from("Inter 12")),
            ..SessionOptions::default()
        };
        let css = session_css(&options, &config.launcher).expect("font produces css");
        assert!(css.contains("font-family: \"Inter\""), "{css}");
        assert!(css.contains("font-size: 12pt"), "{css}");
        assert!(
            css.contains(".launcher-surface"),
            "a preset must not reach the rest of the shell: {css}"
        );
    }

    #[test]
    fn a_style_preset_is_looked_up_by_name_and_a_missing_one_adds_nothing() {
        let config = launcher_config();
        config
            .launcher
            .styles
            .set(std::collections::BTreeMap::from([(
                String::from("compact"),
                String::from(".launcher-row { padding: 0; }"),
            )]));

        let found = session_css(
            &SessionOptions {
                style: Some(String::from("compact")),
                ..SessionOptions::default()
            },
            &config.launcher,
        );
        assert_eq!(found.as_deref(), Some(".launcher-row { padding: 0; }"));

        let missing = session_css(
            &SessionOptions {
                style: Some(String::from("nope")),
                ..SessionOptions::default()
            },
            &config.launcher,
        );
        assert!(
            missing.is_none(),
            "an unknown preset contributes nothing rather than something arbitrary"
        );
    }

    #[test]
    fn the_font_and_a_preset_share_one_provider() {
        let config = launcher_config();
        config
            .launcher
            .styles
            .set(std::collections::BTreeMap::from([(
                String::from("compact"),
                String::from(".launcher-row { padding: 0; }"),
            )]));
        let css = session_css(
            &SessionOptions {
                font: Some(String::from("Inter 12")),
                style: Some(String::from("compact")),
                ..SessionOptions::default()
            },
            &config.launcher,
        )
        .expect("both produce css");
        assert!(css.contains("font-family: \"Inter\""), "{css}");
        assert!(css.contains(".launcher-row"), "{css}");
    }

    #[test]
    fn a_pango_font_description_becomes_css_properties_not_the_css_shorthand() {
        // The bug this exists to catch: `font: Monospace 20` is not valid CSS
        // — CSS wants the size first — so GTK dropped the whole declaration
        // and `-font` did nothing visible.
        let properties = font_properties("Monospace 20");
        assert!(
            properties.contains("font-family: \"Monospace\""),
            "{properties}"
        );
        assert!(properties.contains("font-size: 20pt"), "{properties}");
        assert!(
            !properties.contains("font:"),
            "the shorthand is what silently failed: {properties}"
        );
    }

    #[test]
    fn a_font_description_carries_its_style_and_weight_across() {
        let properties = font_properties("Inter Bold Italic 11");
        assert!(
            properties.contains("font-family: \"Inter\""),
            "{properties}"
        );
        assert!(properties.contains("font-style: italic"), "{properties}");
        assert!(properties.contains("font-weight: 700"), "{properties}");
        // A plain description says nothing about style or weight, so the CSS
        // must not either — otherwise it overrides the theme with defaults.
        let plain = font_properties("Inter 11");
        assert!(!plain.contains("font-style"), "{plain}");
        assert!(!plain.contains("font-weight"), "{plain}");
    }

    #[test]
    fn a_sizeless_font_description_sets_only_the_family() {
        // rofi accepts a bare family; a `font-size: 0pt` would make the
        // launcher unreadable.
        let properties = font_properties("Inter");
        assert_eq!(properties, "font-family: \"Inter\";");
    }
}
