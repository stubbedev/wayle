mod account;

pub use account::{MailAccount, MailProvider};
use schemars::schema_for;
use wayle_derive::wayle_config;

use crate::{
    ClickAction, ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::styling::{ColorValue, CssToken},
};

/// Unread mail count, backed by a notmuch query.
///
/// Runs `notmuch count <query>` and re-queries whenever the maildir changes
/// (event-driven via an inotify watch on the notmuch database path). Hidden
/// while the count is zero when `hide-when-zero` is set.
#[wayle_config(bar_button, i18n_prefix = "settings-modules-mail")]
pub struct MailConfig {
    /// Format string for the label.
    ///
    /// ## Placeholders
    ///
    /// - `{{ count }}` - Number of messages matching the query
    ///
    /// ## Examples
    ///
    /// - `"{{ count }}"` - "3"
    #[serde(rename = "format")]
    #[default(String::from("{{ count }}"))]
    pub format: ConfigProperty<String>,

    /// notmuch search query whose match count is shown.
    ///
    /// Any query `notmuch count` accepts, e.g. `tag:unread`,
    /// `tag:unread and tag:inbox`, `folder:work and tag:unread`.
    ///
    /// Ignored when `accounts` is non-empty — the bar count is then the sum of
    /// the per-account queries.
    #[serde(rename = "query")]
    #[default(String::from("tag:unread"))]
    pub query: ConfigProperty<String>,

    /// Per-account unread breakdown shown in the mail dropdown. Each account
    /// has its own notmuch query and a provider (for its icon). When set, the
    /// bar count/label is the sum across accounts and `query` is ignored.
    #[serde(rename = "accounts")]
    #[default(Vec::new())]
    pub accounts: ConfigProperty<Vec<MailAccount>>,

    /// Hide the module entirely while the count is zero.
    #[serde(rename = "hide-when-zero")]
    #[default(true)]
    pub hide_when_zero: ConfigProperty<bool>,

    /// Fire a desktop notification when the unread count
    /// rises — i.e. new mail arrives. One notification per newly-arrived
    /// message (capped per burst), showing its sender and subject. With
    /// `accounts` configured, each notification uses that account's provider
    /// icon; otherwise the module icon is used.
    #[serde(rename = "notify")]
    #[default(false)]
    pub notify: ConfigProperty<bool>,

    /// Notification summary when new mail arrives.
    ///
    /// ## Placeholders
    ///
    /// - `{{ sender }}` - Message sender (name or address)
    /// - `{{ subject }}` - Message subject
    /// - `{{ count }}` - Total messages matching the query
    /// - `{{ new }}` - How many arrived since the last count
    #[serde(rename = "notify-summary")]
    #[default(String::from("{{ sender }}"))]
    pub notify_summary: ConfigProperty<String>,

    /// Notification body when new mail arrives. Same placeholders as
    /// `notify-summary`.
    #[serde(rename = "notify-body")]
    #[default(String::from("{{ subject }}"))]
    pub notify_body: ConfigProperty<String>,

    /// Module icon.
    #[serde(rename = "icon-name")]
    #[default(String::from("ld-mail-symbolic"))]
    pub icon_name: ConfigProperty<String>,

    /// Display border around button.
    #[serde(rename = "border-show")]
    #[default(false)]
    pub border_show: ConfigProperty<bool>,

    /// Border color token.
    #[serde(rename = "border-color")]
    #[default(ColorValue::Token(CssToken::Blue))]
    pub border_color: ConfigProperty<ColorValue>,

    /// Display module icon.
    #[serde(rename = "icon-show")]
    #[default(true)]
    pub icon_show: ConfigProperty<bool>,

    /// Icon foreground color. Auto selects based on variant for contrast.
    #[serde(rename = "icon-color")]
    #[default(ColorValue::Auto)]
    pub icon_color: ConfigProperty<ColorValue>,

    /// Icon container background color token.
    #[serde(rename = "icon-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub icon_bg_color: ConfigProperty<ColorValue>,

    /// Display label.
    #[serde(rename = "label-show")]
    #[default(true)]
    pub label_show: ConfigProperty<bool>,

    /// Label text color token.
    #[serde(rename = "label-color")]
    #[default(ColorValue::Auto)]
    pub label_color: ConfigProperty<ColorValue>,

    /// Max label characters before truncation with ellipsis. Set to 0 to disable.
    #[serde(rename = "label-max-length")]
    #[default(0)]
    pub label_max_length: ConfigProperty<u32>,

    /// Button background color token.
    #[serde(rename = "button-bg-color")]
    #[default(ColorValue::Token(CssToken::BgSurfaceElevated))]
    pub button_bg_color: ConfigProperty<ColorValue>,

    /// Action on left click. Defaults to opening the per-account dropdown; set
    /// to empty for no action, or a shell command (e.g. your mail client).
    #[serde(rename = "left-click")]
    #[default(ClickAction::Dropdown(String::from("mail")))]
    pub left_click: ConfigProperty<ClickAction>,

    /// Action on right click.
    #[serde(rename = "right-click")]
    #[default(ClickAction::None)]
    pub right_click: ConfigProperty<ClickAction>,

    /// Action on middle click.
    #[serde(rename = "middle-click")]
    #[default(ClickAction::None)]
    pub middle_click: ConfigProperty<ClickAction>,

    /// Action on scroll up.
    #[serde(rename = "scroll-up")]
    #[default(ClickAction::None)]
    pub scroll_up: ConfigProperty<ClickAction>,

    /// Action on scroll down.
    #[serde(rename = "scroll-down")]
    #[default(ClickAction::None)]
    pub scroll_down: ConfigProperty<ClickAction>,
}

impl ModuleInfoProvider for MailConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("mail"),
            schema: || schema_for!(MailConfig),
            layout_id: Some(String::from("mail")),
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::bar_button()
    }
}

crate::register_module!(MailConfig);
