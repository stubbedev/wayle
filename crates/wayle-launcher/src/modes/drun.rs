//! drun mode: launch applications from desktop entries.

use std::{collections::HashMap, path::PathBuf};

use async_trait::async_trait;
use gio::prelude::*;
use gio_unix::DesktopAppInfo;
use tracing::warn;

use crate::{
    history::HistoryStore,
    item::{IconSource, Item},
    mode::{Action, ActivateKind, Mode, ModeState},
    spawn, template,
};

/// Desktop-entry fields searchable via `drun-match-fields`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DrunField {
    /// Localized Name.
    Name,
    /// GenericName.
    Generic,
    /// Exec command line.
    Exec,
    /// Categories list.
    Categories,
    /// Comment.
    Comment,
    /// Keywords list.
    Keywords,
}

/// drun behavior knobs (mirrors rofi's `-drun-*` family).
#[derive(Debug, Clone)]
pub struct DrunConfig {
    /// Only show entries within these categories (empty = all).
    pub categories: Vec<String>,
    /// Hide entries within these categories.
    pub exclude_categories: Vec<String>,
    /// Fields fed to the matcher.
    pub match_fields: Vec<DrunField>,
    /// rofi `drun-display-format` template
    /// (`{name}/{generic}/{exec}/{categories}/{comment}`, `[..]` optional).
    pub display_format: String,
    /// Expose desktop-file actions as extra rows.
    pub show_actions: bool,
    /// Command for `Type=Link` entries (`{url}` placeholder or appended).
    pub url_launcher: String,
    /// Terminal for `Terminal=true` entries ("" = autodetect).
    pub terminal: String,
    /// History cap (rofi `max-history-size`).
    pub max_history: u32,
}

impl Default for DrunConfig {
    fn default() -> Self {
        Self {
            categories: Vec::new(),
            exclude_categories: Vec::new(),
            match_fields: vec![
                DrunField::Name,
                DrunField::Generic,
                DrunField::Exec,
                DrunField::Categories,
                DrunField::Keywords,
            ],
            display_format: "{name} [<span weight='light' size='small'><i>({generic})</i></span>]"
                .to_owned(),
            show_actions: false,
            url_launcher: "xdg-open".to_owned(),
            terminal: String::new(),
            max_history: 25,
        }
    }
}

enum Entry {
    App {
        desktop_id: String,
        action: Option<String>,
    },
    Link {
        desktop_id: String,
        url: String,
    },
}

/// Application launcher mode over freedesktop desktop entries.
pub struct DrunMode {
    config: DrunConfig,
    history: Option<HistoryStore>,
    entries: Vec<Entry>,
}

impl DrunMode {
    /// Create the mode. `history` enables frecency ordering + recording.
    pub fn new(config: DrunConfig, history: Option<HistoryStore>) -> Self {
        Self {
            config,
            history,
            entries: Vec::new(),
        }
    }

    fn frecency(&self) -> HashMap<String, f64> {
        self.history
            .as_ref()
            .and_then(|store| store.frecency("drun").ok())
            .unwrap_or_default()
    }

    fn record(&self, desktop_id: &str) {
        if let Some(store) = &self.history
            && let Err(error) = store.record("drun", desktop_id, self.config.max_history)
        {
            warn!(%error, "drun history record failed");
        }
    }

    fn launch(&self, entry: &Entry) {
        match entry {
            Entry::Link { url, .. } => {
                spawn::run_shell(&format!(
                    "{} {}",
                    self.config.url_launcher,
                    shlex::try_quote(url).unwrap_or(std::borrow::Cow::Borrowed(url.as_str()))
                ));
            }
            Entry::App { desktop_id, action } => {
                let Some(app) = DesktopAppInfo::new(desktop_id) else {
                    warn!(%desktop_id, "desktop entry vanished");
                    return;
                };
                if let Some(action) = action {
                    app.launch_action(action, gio::AppLaunchContext::NONE);
                } else if app.boolean("Terminal") {
                    launch_in_terminal(&app, &self.config.terminal);
                } else if let Err(error) = app.launch(&[], gio::AppLaunchContext::NONE) {
                    warn!(%desktop_id, %error, "app launch failed");
                }
            }
        }
    }
}

#[async_trait]
impl Mode for DrunMode {
    fn name(&self) -> &str {
        "drun"
    }

    async fn load(&mut self) -> ModeState {
        let frecency = self.frecency();
        let mut rows = collect_apps(&self.config);
        rows.extend(collect_links(&self.config));
        rows.sort_by(|a, b| {
            let wa = frecency.get(entry_id(&a.1)).copied().unwrap_or(0.0);
            let wb = frecency.get(entry_id(&b.1)).copied().unwrap_or(0.0);
            wb.partial_cmp(&wa)
                .unwrap_or(std::cmp::Ordering::Equal)
                .then_with(|| a.0.display.to_lowercase().cmp(&b.0.display.to_lowercase()))
        });
        let (items, entries): (Vec<Item>, Vec<Entry>) = rows.into_iter().unzip();
        self.entries = entries;
        ModeState {
            items,
            prompt: "drun".to_owned(),
            markup_rows: true,
            ..ModeState::default()
        }
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, _input: &str) -> Action {
        match (index, kind) {
            (Some(row), _) => {
                let Some(entry) = self.entries.get(row as usize) else {
                    return Action::Nothing;
                };
                self.record(entry_id(entry));
                self.launch(entry);
                Action::Close
            }
            // rofi drun runs custom input as a command.
            (None, ActivateKind::Custom(input)) => {
                spawn::run_shell(&input);
                Action::Close
            }
            (None, _) => Action::Nothing,
        }
    }

    async fn delete(&mut self, index: u32) -> Action {
        if let (Some(store), Some(entry)) = (&self.history, self.entries.get(index as usize)) {
            if let Err(error) = store.remove("drun", entry_id(entry)) {
                warn!(%error, "drun history delete failed");
            }
            return Action::Reload(self.load().await);
        }
        Action::Nothing
    }
}

fn entry_id(entry: &Entry) -> &str {
    match entry {
        Entry::App { desktop_id, action } => action
            .as_deref()
            .map_or(desktop_id.as_str(), |_| desktop_id.as_str()),
        Entry::Link { desktop_id, .. } => desktop_id,
    }
}

/// Category allow/deny filtering (rofi `-drun-categories` /
/// `-drun-exclude-categories`).
fn category_allowed(categories: &str, config: &DrunConfig) -> bool {
    let list: Vec<&str> = categories.split(';').filter(|c| !c.is_empty()).collect();
    if list
        .iter()
        .any(|c| config.exclude_categories.iter().any(|e| e == c))
    {
        return false;
    }
    config.categories.is_empty()
        || list
            .iter()
            .any(|c| config.categories.iter().any(|w| w == c))
}

fn collect_apps(config: &DrunConfig) -> Vec<(Item, Entry)> {
    let mut rows = Vec::new();
    for app in gio::AppInfo::all() {
        let Ok(app) = app.downcast::<DesktopAppInfo>() else {
            continue;
        };
        if !app.should_show() {
            continue;
        }
        let categories = app.categories().unwrap_or_default().to_string();
        if !category_allowed(&categories, config) {
            continue;
        }
        let Some(desktop_id) = app.id().map(|id| id.to_string()) else {
            continue;
        };
        rows.push((
            app_item(&app, None, config),
            Entry::App {
                desktop_id: desktop_id.clone(),
                action: None,
            },
        ));
        if config.show_actions {
            for action in app.list_actions() {
                rows.push((
                    app_item(&app, Some(action.as_str()), config),
                    Entry::App {
                        desktop_id: desktop_id.clone(),
                        action: Some(action.to_string()),
                    },
                ));
            }
        }
    }
    rows
}

fn app_item(app: &DesktopAppInfo, action: Option<&str>, config: &DrunConfig) -> Item {
    let name = match action {
        Some(action_id) => format!("{} ({})", app.display_name(), app.action_name(action_id)),
        None => app.display_name().to_string(),
    };
    let generic = app.generic_name().unwrap_or_default().to_string();
    let exec = app
        .commandline()
        .map(|path| path.display().to_string())
        .unwrap_or_default();
    let categories = app.categories().unwrap_or_default().to_string();
    let comment = app.description().unwrap_or_default().to_string();
    let keywords = app
        .keywords()
        .iter()
        .map(ToString::to_string)
        .collect::<Vec<_>>()
        .join(" ");

    let display = template::render(&config.display_format, |key| match key {
        "name" => Some(glib::markup_escape_text(&name).to_string()),
        "generic" => Some(glib::markup_escape_text(&generic).to_string()),
        "exec" => Some(glib::markup_escape_text(&exec).to_string()),
        "categories" => Some(glib::markup_escape_text(&categories).to_string()),
        "comment" => Some(glib::markup_escape_text(&comment).to_string()),
        _ => None,
    });
    let match_text = build_match_text(
        &config.match_fields,
        &[
            (DrunField::Name, name.as_str()),
            (DrunField::Generic, generic.as_str()),
            (DrunField::Exec, exec.as_str()),
            (DrunField::Categories, categories.as_str()),
            (DrunField::Comment, comment.as_str()),
            (DrunField::Keywords, keywords.as_str()),
        ],
    );

    Item {
        display,
        match_text,
        icon: app.string("Icon").map(|raw| icon_source(raw.as_str())),
        info: None,
        flags: crate::item::ItemFlags::MARKUP,
    }
}

fn build_match_text(fields: &[DrunField], values: &[(DrunField, &str)]) -> String {
    values
        .iter()
        .filter(|(field, value)| fields.contains(field) && !value.is_empty())
        .map(|(_, value)| *value)
        .collect::<Vec<_>>()
        .join(" ")
}

fn icon_source(raw: &str) -> IconSource {
    if raw.starts_with('/') {
        IconSource::File(PathBuf::from(raw))
    } else {
        IconSource::Name(raw.to_owned())
    }
}

/// `Terminal=true`: run the Exec line (field codes stripped) inside a
/// terminal, since gio's own terminal lookup rarely fits Wayland setups.
fn launch_in_terminal(app: &DesktopAppInfo, configured_terminal: &str) {
    let exec = app.string("Exec").unwrap_or_default().to_string();
    let Some(mut argv) = shlex::split(&exec) else {
        warn!(%exec, "unparseable Exec line");
        return;
    };
    argv.retain(|token| !is_field_code(token));
    let terminal = spawn::detect_terminal(configured_terminal);
    let mut command = vec![terminal, "-e".to_owned()];
    command.extend(argv);
    spawn::run_argv(&command);
}

fn is_field_code(token: &str) -> bool {
    matches!(
        token,
        "%f" | "%F" | "%u" | "%U" | "%d" | "%D" | "%n" | "%N" | "%i" | "%c" | "%k" | "%v" | "%m"
    )
}

/// `Type=Link` desktop entries — gio's `AppInfo::all()` skips them, rofi
/// shows them and opens the URL via `-drun-url-launcher`.
fn collect_links(config: &DrunConfig) -> Vec<(Item, Entry)> {
    let mut rows = Vec::new();
    let mut seen = std::collections::HashSet::new();
    for dir in xdg_application_dirs() {
        let Ok(read) = std::fs::read_dir(&dir) else {
            continue;
        };
        for file in read.flatten() {
            let path = file.path();
            if path.extension().and_then(|e| e.to_str()) != Some("desktop") {
                continue;
            }
            let Some(id) = path.file_name().and_then(|n| n.to_str()) else {
                continue;
            };
            if !seen.insert(id.to_owned()) {
                continue; // earlier XDG dir wins
            }
            if let Some(row) = link_row(&path, id, config) {
                rows.push(row);
            }
        }
    }
    rows
}

fn link_row(path: &std::path::Path, id: &str, config: &DrunConfig) -> Option<(Item, Entry)> {
    let keyfile = glib::KeyFile::new();
    keyfile
        .load_from_file(path, glib::KeyFileFlags::NONE)
        .ok()?;
    let group = "Desktop Entry";
    if keyfile.string(group, "Type").ok()?.as_str() != "Link" {
        return None;
    }
    if keyfile.boolean(group, "NoDisplay").unwrap_or(false)
        || keyfile.boolean(group, "Hidden").unwrap_or(false)
    {
        return None;
    }
    let name = keyfile.locale_string(group, "Name", None).ok()?.to_string();
    let url = keyfile.string(group, "URL").ok()?.to_string();
    let display = template::render(&config.display_format, |key| match key {
        "name" => Some(glib::markup_escape_text(&name).to_string()),
        _ => None,
    });
    let item = Item {
        display,
        match_text: format!("{name} {url}"),
        icon: keyfile
            .string(group, "Icon")
            .ok()
            .map(|raw| icon_source(raw.as_str())),
        info: None,
        flags: crate::item::ItemFlags::MARKUP,
    };
    Some((
        item,
        Entry::Link {
            desktop_id: id.to_owned(),
            url,
        },
    ))
}

fn xdg_application_dirs() -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    if let Some(data_home) = std::env::var_os("XDG_DATA_HOME") {
        dirs.push(PathBuf::from(data_home).join("applications"));
    } else if let Some(home) = std::env::var_os("HOME") {
        dirs.push(PathBuf::from(home).join(".local/share/applications"));
    }
    let data_dirs =
        std::env::var("XDG_DATA_DIRS").unwrap_or_else(|_| "/usr/local/share:/usr/share".into());
    for dir in data_dirs.split(':').filter(|d| !d.is_empty()) {
        dirs.push(PathBuf::from(dir).join("applications"));
    }
    dirs
}

#[cfg(test)]
mod tests {
    use super::*;

    fn config() -> DrunConfig {
        DrunConfig::default()
    }

    #[test]
    fn category_filtering() {
        let mut cfg = config();
        assert!(category_allowed("Network;WebBrowser;", &cfg));
        cfg.exclude_categories = vec!["WebBrowser".into()];
        assert!(!category_allowed("Network;WebBrowser;", &cfg));
        cfg.exclude_categories.clear();
        cfg.categories = vec!["Development".into()];
        assert!(!category_allowed("Network;WebBrowser;", &cfg));
        assert!(category_allowed("Development;IDE;", &cfg));
    }

    #[test]
    fn field_codes_detected() {
        assert!(is_field_code("%U"));
        assert!(!is_field_code("100%"));
        assert!(!is_field_code("file.txt"));
    }

    #[test]
    fn match_text_respects_selected_fields() {
        let text = build_match_text(
            &[DrunField::Name, DrunField::Keywords],
            &[
                (DrunField::Name, "Firefox"),
                (DrunField::Exec, "firefox"),
                (DrunField::Keywords, "web browser"),
            ],
        );
        assert_eq!(text, "Firefox web browser");
    }

    #[test]
    fn link_rows_parsed_from_desktop_file() {
        let dir = std::env::temp_dir().join(format!("wayle-drun-test-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("example.desktop");
        std::fs::write(
            &path,
            "[Desktop Entry]\nType=Link\nName=Example\nURL=https://example.com\n",
        )
        .unwrap();
        let row = link_row(&path, "example.desktop", &config()).unwrap();
        assert_eq!(row.0.match_text, "Example https://example.com");
        assert!(matches!(row.1, Entry::Link { ref url, .. } if url == "https://example.com"));
        std::fs::remove_dir_all(&dir).ok();
    }
}
