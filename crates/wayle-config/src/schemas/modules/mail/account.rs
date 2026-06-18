use schemars::{JsonSchema, schema_for};
use serde::{Deserialize, Serialize};

use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};

/// Mail provider, used to pick a default brand icon for an account.
#[derive(
    Debug,
    Clone,
    Copy,
    Default,
    PartialEq,
    Eq,
    Serialize,
    Deserialize,
    JsonSchema,
    wayle_derive::EnumVariants,
)]
#[serde(rename_all = "kebab-case")]
pub enum MailProvider {
    /// Generic mailbox; uses the plain mail glyph.
    #[default]
    Generic,
    /// Gmail.
    Gmail,
    /// Microsoft Outlook.
    Outlook,
    /// Apple iCloud Mail.
    Icloud,
    /// Proton Mail.
    Proton,
    /// Fastmail.
    Fastmail,
    /// Yahoo Mail.
    Yahoo,
}

impl MailProvider {
    /// Default symbolic icon name for this provider. Brand icons come from the
    /// Simple Icons set (`si-*`); the generic provider uses the mail glyph.
    #[must_use]
    pub fn default_icon(self) -> &'static str {
        match self {
            Self::Generic => "ld-mail-symbolic",
            Self::Gmail => "si-gmail-symbolic",
            Self::Outlook => "si-microsoftoutlook-symbolic",
            Self::Icloud => "si-icloud-symbolic",
            Self::Proton => "si-protonmail-symbolic",
            Self::Fastmail => "si-fastmail-symbolic",
            Self::Yahoo => "si-yahoo-symbolic",
        }
    }

    /// Simple Icons slug used to install this provider's brand icon, if any.
    #[must_use]
    pub fn simple_icons_slug(self) -> Option<&'static str> {
        match self {
            Self::Generic => None,
            Self::Gmail => Some("gmail"),
            Self::Outlook => Some("microsoftoutlook"),
            Self::Icloud => Some("icloud"),
            Self::Proton => Some("protonmail"),
            Self::Fastmail => Some("fastmail"),
            Self::Yahoo => Some("yahoo"),
        }
    }
}

/// One mail account in the `[modules.mail]` per-account breakdown.
///
/// Each account has its own notmuch query; the dropdown shows the per-account
/// unread counts and the bar shows their sum.
///
/// ## Example
///
/// ```toml
/// [[modules.mail.accounts]]
/// name = "Work"
/// query = "folder:work/INBOX and tag:unread"
/// provider = "gmail"
/// ```
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "kebab-case")]
pub struct MailAccount {
    /// Display name shown in the dropdown.
    pub name: String,

    /// notmuch query whose match count is this account's unread total.
    pub query: String,

    /// Provider, selecting the default brand icon.
    #[serde(default)]
    pub provider: MailProvider,

    /// Optional icon override. Empty uses the provider's default icon.
    #[serde(default)]
    pub icon: Option<String>,
}

impl MailAccount {
    /// Resolved icon: an explicit non-empty override, else the provider default.
    #[must_use]
    pub fn resolved_icon(&self) -> String {
        self.icon
            .as_deref()
            .filter(|s| !s.is_empty())
            .map_or_else(
                || self.provider.default_icon().to_owned(),
                ToOwned::to_owned,
            )
    }
}

impl ModuleInfoProvider for MailAccount {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("mail-account"),
            schema: || schema_for!(MailAccount),
            layout_id: None,
            array_entry: true,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

crate::register_module!(MailAccount);
