//! Pure helpers: label rendering, workspace-map lookup, ignore matching,
//! and CSS class naming.

use std::collections::BTreeMap;

use wayle_config::schemas::modules::{LabelStrategy, WorkspaceStyle};

use crate::{glob, shell::bar::icons::DEFAULT_APP_ICON_MAP};

const TITLE_PREFIX: &str = "title:";
const APP_PREFIX: &str = "app:";

/// Renders the label for a workspace per the configured strategy.
///
/// `num` is sway's workspace number, or `-1` for purely named workspaces; the
/// number is only shown when non-negative.
///
/// Returns `None` only for [`LabelStrategy::NameOnly`] when the workspace has
/// no name set, or for [`LabelStrategy::Index`] on an unnumbered workspace.
pub(super) fn label_for(num: i32, name: Option<&str>, strategy: LabelStrategy) -> Option<String> {
    let num_label = (num >= 0).then(|| num.to_string());

    match strategy {
        LabelStrategy::Index => num_label,
        LabelStrategy::NameOrIndex => name.map(String::from).or(num_label),
        LabelStrategy::NameOnly => name.map(String::from),
        LabelStrategy::IndexAndName => match (num_label, name) {
            (Some(num), Some(name)) => Some(format!("{num}: {name}")),
            (Some(num), None) => Some(num),
            (None, Some(name)) => Some(name.to_owned()),
            (None, None) => None,
        },
    }
}

/// Looks up a per-workspace style override.
///
/// Tries the workspace name first, then the stable id rendered as a string.
pub(super) fn workspace_style<'a>(
    name: Option<&str>,
    id: u64,
    map: &'a BTreeMap<String, WorkspaceStyle>,
) -> Option<&'a WorkspaceStyle> {
    if let Some(name) = name
        && let Some(style) = map.get(name)
    {
        return Some(style);
    }
    map.get(&id.to_string())
}

/// CSS class used to address a workspace button by id, e.g. `ws-id-5`.
pub(super) fn workspace_id_css_class(id: u64) -> String {
    format!("ws-id-{id}")
}

/// CSS class used to address a workspace button by name, e.g. `ws-name-web`.
///
/// Non-identifier characters in the name are replaced with `_` so the
/// resulting class is always a valid CSS identifier.
pub(super) fn workspace_name_css_class(name: &str) -> String {
    let sanitized: String = name
        .chars()
        .map(|character| {
            if character.is_ascii_alphanumeric() || character == '-' || character == '_' {
                character
            } else {
                '_'
            }
        })
        .collect();
    format!("ws-name-{sanitized}")
}

/// Returns `true` when the workspace matches any of the ignore patterns.
///
/// Patterns are tried against the name, then the number, then the stable id.
pub(super) fn is_ignored(name: Option<&str>, num: i32, id: u64, patterns: &[String]) -> bool {
    let num_str = num.to_string();
    let id_str = id.to_string();

    patterns.iter().any(|pattern| {
        if let Some(name) = name
            && glob::matches(pattern, name)
        {
            return true;
        }
        glob::matches(pattern, &num_str) || glob::matches(pattern, &id_str)
    })
}

/// Resolves the icon name for a window using the configured icon map.
///
/// Lookup order: title-prefixed patterns against `title`, then app-prefixed
/// or unprefixed patterns against `app_id`. Falls back to `fallback` when
/// nothing matches.
pub(super) fn resolve_app_icon(
    app_id: Option<&str>,
    title: Option<&str>,
    user_map: &BTreeMap<String, String>,
    fallback: &str,
) -> String {
    let (title_entries, app_entries): (Vec<_>, Vec<_>) = user_map
        .iter()
        .partition(|(pattern, _)| pattern.starts_with(TITLE_PREFIX));

    if let Some(title) = title
        && let Some(icon) = match_prefixed(&title_entries, TITLE_PREFIX, title)
    {
        return icon.to_string();
    }

    let Some(app_id) = app_id else {
        return fallback.to_string();
    };

    if let Some(icon) = match_prefixed(&app_entries, APP_PREFIX, app_id) {
        return icon.to_string();
    }

    if let Some(icon) = glob::find_match(DEFAULT_APP_ICON_MAP.iter().copied(), app_id) {
        return icon.to_string();
    }

    fallback.to_string()
}

/// Matches `query` against `entries`, stripping `prefix` from each pattern
/// first, and returns the matched icon name.
fn match_prefixed<'a>(
    entries: &[(&'a String, &'a String)],
    prefix: &str,
    query: &str,
) -> Option<&'a str> {
    let candidates = entries.iter().map(|(pattern, icon)| {
        let stripped = pattern.strip_prefix(prefix).unwrap_or(pattern);
        (stripped, icon.as_str())
    });

    glob::find_match(candidates, query)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn label_index_only() {
        assert_eq!(
            label_for(3, Some("web"), LabelStrategy::Index),
            Some(String::from("3")),
        );
        assert_eq!(label_for(-1, Some("web"), LabelStrategy::Index), None);
    }

    #[test]
    fn label_name_or_index_uses_name() {
        assert_eq!(
            label_for(3, Some("web"), LabelStrategy::NameOrIndex),
            Some(String::from("web")),
        );
    }

    #[test]
    fn label_name_or_index_falls_back_to_number() {
        assert_eq!(
            label_for(3, None, LabelStrategy::NameOrIndex),
            Some(String::from("3")),
        );
    }

    #[test]
    fn label_name_only_returns_none_when_unset() {
        assert_eq!(label_for(3, None, LabelStrategy::NameOnly), None);
        assert_eq!(
            label_for(3, Some("web"), LabelStrategy::NameOnly),
            Some(String::from("web")),
        );
    }

    #[test]
    fn label_index_and_name_with_name() {
        assert_eq!(
            label_for(3, Some("web"), LabelStrategy::IndexAndName),
            Some(String::from("3: web")),
        );
    }

    #[test]
    fn label_index_and_name_without_name_shows_number_alone() {
        assert_eq!(
            label_for(3, None, LabelStrategy::IndexAndName),
            Some(String::from("3")),
        );
    }

    #[test]
    fn workspace_style_prefers_name_match() {
        let mut map = BTreeMap::new();
        map.insert(
            String::from("web"),
            WorkspaceStyle {
                icon: Some(String::from("by-name")),
                color: None,
                label: None,
            },
        );
        map.insert(
            String::from("5"),
            WorkspaceStyle {
                icon: Some(String::from("by-id")),
                color: None,
                label: None,
            },
        );

        let icon = workspace_style(Some("web"), 5, &map).and_then(|style| style.icon.clone());
        assert_eq!(icon, Some(String::from("by-name")));
    }

    #[test]
    fn ignore_matches_by_name() {
        let patterns = vec![String::from("scratch")];
        assert!(is_ignored(Some("scratch"), 5, 12, &patterns));
    }

    #[test]
    fn ignore_matches_by_number_glob() {
        let patterns = vec![String::from("1?")];
        assert!(is_ignored(None, 12, 99, &patterns));
        assert!(!is_ignored(None, 5, 99, &patterns));
    }

    #[test]
    fn ignore_no_match() {
        let patterns = vec![String::from("scratch"), String::from("foo")];
        assert!(!is_ignored(Some("web"), 1, 2, &patterns));
    }

    #[test]
    fn resolve_app_icon_title_takes_priority_over_app() {
        let mut map = BTreeMap::new();
        map.insert(String::from("title:*YouTube*"), String::from("ld-youtube"));
        map.insert(String::from("*firefox*"), String::from("ld-globe"));
        assert_eq!(
            resolve_app_icon(
                Some("org.mozilla.firefox"),
                Some("YouTube - Firefox"),
                &map,
                "fallback",
            ),
            "ld-youtube",
        );
    }

    #[test]
    fn resolve_app_icon_falls_back_when_no_match() {
        let map = BTreeMap::new();
        assert_eq!(
            resolve_app_icon(Some("unknown.app"), Some("Unknown"), &map, "ld-default"),
            "ld-default",
        );
    }
}
