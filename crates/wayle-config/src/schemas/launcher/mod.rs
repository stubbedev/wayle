mod types;

use std::collections::BTreeMap;

#[cfg(feature = "schema")]
use schemars::schema_for;
pub use types::{
    LauncherCase, LauncherDrunField, LauncherFileSort, LauncherLocation, LauncherMatching,
    LauncherSorting, LauncherWindowField,
};
use wayle_derive::wayle_config;

#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{ConfigProperty, schemas::styling::Size};

/// Base width (in rem, `1rem = 16px`) a `Size::Scale` multiplier resolves
/// against (37.5rem = 600px).
pub const WIDTH_BASE_REM: f32 = 37.5;

/// Application launcher / dmenu (rofi replacement).
#[wayle_config(i18n_prefix = "settings-launcher")]
pub struct LauncherConfig {
    /// Surface position on screen.
    #[default(LauncherLocation::default())]
    pub location: ConfigProperty<LauncherLocation>,

    /// Surface width: a multiplier of the default 600px (`1.0` = default)
    /// or absolute pixels (e.g. `"800px"`).
    #[default(Size::scale(1.0))]
    pub width: ConfigProperty<Size>,

    /// Visible result lines.
    #[default(10u32)]
    pub lines: ConfigProperty<u32>,

    /// Output connector to show on ("" = focused output).
    #[default(String::new())]
    pub monitor: ConfigProperty<String>,

    /// Modes enabled by default (order = tab/kb-mode-next order). Script
    /// modes are referenced by their `[launcher.scripts]` key.
    #[default(vec![String::from("drun"), String::from("run"), String::from("window")])]
    pub modes: ConfigProperty<Vec<String>>,

    /// Wrap selection at list edges.
    #[default(true)]
    pub cycle: ConfigProperty<bool>,

    /// Matching method.
    #[default(LauncherMatching::default())]
    pub matching: ConfigProperty<LauncherMatching>,

    /// Split the query into independently matched words.
    #[default(true)]
    pub tokenize: ConfigProperty<bool>,

    /// Token prefix that negates a token.
    #[serde(rename = "negate-char")]
    #[default(String::from("-"))]
    pub negate_char: ConfigProperty<String>,

    /// Strip accents/normalize Unicode while matching.
    #[serde(rename = "normalize-match")]
    #[default(true)]
    pub normalize_match: ConfigProperty<bool>,

    /// Rank results by match quality (off = keep list order, rofi default).
    #[default(false)]
    pub sort: ConfigProperty<bool>,

    /// Ranking method when `sort` is on.
    #[serde(rename = "sorting-method")]
    #[default(LauncherSorting::default())]
    pub sorting_method: ConfigProperty<LauncherSorting>,

    /// Case handling.
    #[default(LauncherCase::default())]
    pub case: ConfigProperty<LauncherCase>,

    /// Terminal emulator for terminal apps ("" = autodetect).
    #[default(String::new())]
    pub terminal: ConfigProperty<String>,

    /// Show row icons.
    #[serde(rename = "show-icons")]
    #[default(true)]
    pub show_icons: ConfigProperty<bool>,

    /// Icon theme override ("" = system theme).
    #[serde(rename = "icon-theme")]
    #[default(String::new())]
    pub icon_theme: ConfigProperty<String>,

    /// Show mode tabs at the bottom of the surface.
    #[serde(rename = "sidebar-mode")]
    #[default(false)]
    pub sidebar_mode: ConfigProperty<bool>,

    /// Accept automatically when exactly one result remains.
    #[serde(rename = "auto-select")]
    #[default(false)]
    pub auto_select: ConfigProperty<bool>,

    /// Select the row under the mouse cursor.
    #[serde(rename = "hover-select")]
    #[default(false)]
    pub hover_select: ConfigProperty<bool>,

    /// Keep the list height fixed at `lines` rows.
    #[serde(rename = "fixed-num-lines")]
    #[default(true)]
    pub fixed_num_lines: ConfigProperty<bool>,

    /// Per-mode display-name overrides (rofi `display-{mode}`), e.g.
    /// `drun = "apps"`.
    #[serde(rename = "display-names")]
    #[default(BTreeMap::new())]
    pub display_names: ConfigProperty<BTreeMap<String, String>>,

    /// Custom script modes: name → executable (rofi `name:script`).
    #[default(BTreeMap::new())]
    pub scripts: ConfigProperty<BTreeMap<String, String>>,

    /// Keybinding overrides: action (rofi `kb-` name without the prefix,
    /// e.g. `accept`, `cancel`, `mode-next`, `custom-1`) → comma-separated
    /// key list (e.g. `"Control+Tab,F3"`). Unset actions keep rofi's
    /// defaults.
    #[default(BTreeMap::new())]
    pub keybindings: ConfigProperty<BTreeMap<String, String>>,

    /// Launch history / frecency.
    pub history: LauncherHistoryConfig,

    /// drun (application) mode.
    pub drun: LauncherDrunConfig,

    /// run (command) mode.
    pub run: LauncherRunConfig,

    /// window switcher mode.
    pub window: LauncherWindowConfig,

    /// ssh mode.
    pub ssh: LauncherSshConfig,

    /// file browser mode.
    pub filebrowser: LauncherFilebrowserConfig,

    /// combi (combined modes) mode.
    pub combi: LauncherCombiConfig,
}

/// Launch history / frecency settings.
#[wayle_config(i18n_prefix = "settings-launcher-history")]
pub struct LauncherHistoryConfig {
    /// Record launches and rank frequently used entries first.
    #[default(true)]
    pub enable: ConfigProperty<bool>,

    /// Maximum remembered entries per mode.
    #[serde(rename = "max-size")]
    #[default(25u32)]
    pub max_size: ConfigProperty<u32>,
}

/// drun (application) mode settings.
#[wayle_config(i18n_prefix = "settings-launcher-drun")]
pub struct LauncherDrunConfig {
    /// Only show apps within these categories (empty = all).
    #[default(Vec::new())]
    pub categories: ConfigProperty<Vec<String>>,

    /// Hide apps within these categories.
    #[serde(rename = "exclude-categories")]
    #[default(Vec::new())]
    pub exclude_categories: ConfigProperty<Vec<String>>,

    /// Desktop-entry fields fed to the matcher.
    #[serde(rename = "match-fields")]
    #[default(vec![
        LauncherDrunField::Name,
        LauncherDrunField::Generic,
        LauncherDrunField::Exec,
        LauncherDrunField::Categories,
        LauncherDrunField::Keywords,
    ])]
    pub match_fields: ConfigProperty<Vec<LauncherDrunField>>,

    /// Row template: `{name}`, `{generic}`, `{exec}`, `{categories}`,
    /// `{comment}`; `[..]` renders only when its placeholders are non-empty.
    #[serde(rename = "display-format")]
    #[default(String::from(
        "{name} [<span weight='light' size='small'><i>({generic})</i></span>]"
    ))]
    pub display_format: ConfigProperty<String>,

    /// Expose desktop-file actions as extra rows.
    #[serde(rename = "show-actions")]
    #[default(false)]
    pub show_actions: ConfigProperty<bool>,

    /// Command opening `Type=Link` entries.
    #[serde(rename = "url-launcher")]
    #[default(String::from("xdg-open"))]
    pub url_launcher: ConfigProperty<String>,
}

/// run (command) mode settings.
#[wayle_config(i18n_prefix = "settings-launcher-run")]
pub struct LauncherRunConfig {
    /// Plain accept template (`{cmd}`).
    #[serde(rename = "run-command")]
    #[default(String::from("{cmd}"))]
    pub run_command: ConfigProperty<String>,

    /// Run-in-terminal template (`{terminal}`, `{cmd}`).
    #[serde(rename = "shell-command")]
    #[default(String::from("{terminal} -e {cmd}"))]
    pub shell_command: ConfigProperty<String>,

    /// Extra command whose stdout lines add entries.
    #[serde(rename = "list-command")]
    #[default(String::new())]
    pub list_command: ConfigProperty<String>,
}

/// window switcher mode settings.
#[wayle_config(i18n_prefix = "settings-launcher-window")]
pub struct LauncherWindowConfig {
    /// Row template: `{w}` workspace, `{c}` class, `{t}` title, `{n}` name,
    /// `{r}` role.
    #[default(String::from("{w}   {c}   {t}"))]
    pub format: ConfigProperty<String>,

    /// Window fields fed to the matcher.
    #[serde(rename = "match-fields")]
    #[default(vec![LauncherWindowField::Title, LauncherWindowField::Class])]
    pub match_fields: ConfigProperty<Vec<LauncherWindowField>>,

    /// Hide the currently focused window from the list.
    #[serde(rename = "hide-active")]
    #[default(false)]
    pub hide_active: ConfigProperty<bool>,

    /// Shift-delete closes the selected window.
    #[serde(rename = "close-on-delete")]
    #[default(true)]
    pub close_on_delete: ConfigProperty<bool>,
}

/// ssh mode settings.
#[wayle_config(i18n_prefix = "settings-launcher-ssh")]
pub struct LauncherSshConfig {
    /// SSH client binary.
    #[default(String::from("ssh"))]
    pub client: ConfigProperty<String>,

    /// Connect template (`{terminal}`, `{ssh-client}`, `{host}`).
    #[default(String::from("{terminal} -e {ssh-client} {host}"))]
    pub command: ConfigProperty<String>,

    /// Include hosts from `/etc/hosts`.
    #[serde(rename = "parse-hosts")]
    #[default(false)]
    pub parse_hosts: ConfigProperty<bool>,

    /// Include hosts from `~/.ssh/known_hosts`.
    #[serde(rename = "parse-known-hosts")]
    #[default(true)]
    pub parse_known_hosts: ConfigProperty<bool>,
}

/// file browser mode settings.
#[wayle_config(i18n_prefix = "settings-launcher-filebrowser")]
pub struct LauncherFilebrowserConfig {
    /// Start directory ("" = home).
    #[default(String::new())]
    pub directory: ConfigProperty<String>,

    /// File ordering.
    #[serde(rename = "sorting-method")]
    #[default(LauncherFileSort::default())]
    pub sorting_method: ConfigProperty<LauncherFileSort>,

    /// List directories before files.
    #[serde(rename = "directories-first")]
    #[default(true)]
    pub directories_first: ConfigProperty<bool>,

    /// Show hidden files.
    #[serde(rename = "show-hidden")]
    #[default(false)]
    pub show_hidden: ConfigProperty<bool>,

    /// Command opening the picked file ("" = xdg-open).
    #[default(String::new())]
    pub command: ConfigProperty<String>,
}

/// combi mode settings.
#[wayle_config(i18n_prefix = "settings-launcher-combi")]
pub struct LauncherCombiConfig {
    /// Modes merged into the combined list.
    #[default(vec![
        String::from("window"),
        String::from("drun"),
        String::from("run"),
    ])]
    pub modes: ConfigProperty<Vec<String>>,

    /// Row template (`{mode}`, `{text}`).
    #[serde(rename = "display-format")]
    #[default(String::from("{text}"))]
    pub display_format: ConfigProperty<String>,
}

#[cfg(feature = "schema")]
impl ModuleInfoProvider for LauncherConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("launcher"),
            schema: || schema_for!(LauncherConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(LauncherConfig);
