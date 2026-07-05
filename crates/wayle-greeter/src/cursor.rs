//! Cursor theme/size auto-detection.
//!
//! The greeter runs pre-login, so there is no session to ask what cursor the
//! user actually sees. Instead the last logged-in user's own dotfiles are
//! read best-effort (only when their home is readable by the greeter user):
//! Hyprland `env = XCURSOR_*` lines / `hyprctl setcursor`, the niri `cursor`
//! block, sway's `seat ... xcursor_theme`, GTK `settings.ini`, and the XDG
//! `~/.icons/default/index.theme` default. The compositor matching the
//! remembered session is consulted first, so a machine with both a Hyprland
//! and a niri config follows the one the user last logged into.

use std::path::{Path, PathBuf};

/// Cursor settings found by one detection source; either side may be missing
/// (e.g. a Hyprland config that sets only the theme).
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct Cursor {
    /// Xcursor theme name.
    pub theme: Option<String>,
    /// Logical cursor size.
    pub size: Option<u32>,
}

impl Cursor {
    /// Fills whichever sides are missing in `self` from `other`.
    #[must_use]
    pub fn or(self, other: Cursor) -> Cursor {
        Cursor {
            theme: self.theme.or(other.theme),
            size: self.size.or(other.size),
        }
    }

    fn complete(&self) -> bool {
        self.theme.is_some() && self.size.is_some()
    }
}

/// Best-effort read of the cursor the user's own session uses, from dotfiles
/// under `home`. `session` is the remembered session id (e.g. "hyprland",
/// "niri"); the matching compositor's config is consulted first.
pub fn detect(home: &Path, session: &str) -> Cursor {
    let read = |rel: &str| std::fs::read_to_string(home.join(rel)).unwrap_or_default();
    let session = session.to_lowercase();

    let mut compositors = ["hypr", "niri", "sway"];
    if let Some(pos) = compositors.iter().position(|c| session.contains(c)) {
        compositors[..=pos].rotate_right(1);
    }

    let mut found = Cursor::default();
    for compositor in compositors {
        if found.complete() {
            break;
        }
        let parsed = match compositor {
            "hypr" => hyprland_file(&home.join(".config/hypr/hyprland.conf"), home, 0),
            "niri" => parse_niri(&read(".config/niri/config.kdl")),
            _ => parse_sway(&read(".config/sway/config")),
        };
        found = found.or(parsed);
    }
    for parsed in [
        parse_gtk_settings(&read(".config/gtk-4.0/settings.ini")),
        parse_gtk_settings(&read(".config/gtk-3.0/settings.ini")),
        parse_index_theme(&read(".icons/default/index.theme")),
    ] {
        if found.complete() {
            break;
        }
        found = found.or(parsed);
    }
    found
}

/// Reads a Hyprland config file, following `source =` includes (depth-capped;
/// glob patterns in `source` are not expanded).
fn hyprland_file(path: &Path, home: &Path, depth: u8) -> Cursor {
    if depth > 3 {
        return Cursor::default();
    }
    let Ok(text) = std::fs::read_to_string(path) else {
        return Cursor::default();
    };
    let (mut found, sources) = parse_hyprland(&text);
    for source in sources {
        if found.complete() {
            break;
        }
        let resolved = if let Some(rest) = source.strip_prefix("~/") {
            home.join(rest)
        } else if let Some(rest) = source.strip_prefix("$HOME/") {
            home.join(rest)
        } else if source.starts_with('/') {
            PathBuf::from(&source)
        } else {
            // Relative sources resolve against the including file's directory.
            path.parent().unwrap_or(Path::new("")).join(&source)
        };
        found = found.or(hyprland_file(&resolved, home, depth + 1));
    }
    found
}

/// Extracts cursor settings and `source =` include paths from Hyprland config
/// text: `env = XCURSOR_THEME,<t>` / `env = XCURSOR_SIZE,<n>` lines and
/// `exec[-once] = hyprctl setcursor <theme> <size>`.
fn parse_hyprland(text: &str) -> (Cursor, Vec<String>) {
    let mut cursor = Cursor::default();
    let mut sources = Vec::new();
    for line in text.lines() {
        let line = line.split('#').next().unwrap_or_default().trim();
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        let (key, value) = (key.trim(), value.trim());
        match key {
            "env" | "envd" => {
                let Some((name, val)) = value.split_once(',') else {
                    continue;
                };
                match name.trim() {
                    "XCURSOR_THEME" if cursor.theme.is_none() => {
                        cursor.theme = Some(val.trim().to_owned());
                    }
                    "XCURSOR_SIZE" if cursor.size.is_none() => {
                        cursor.size = val.trim().parse().ok();
                    }
                    _ => {}
                }
            }
            "exec" | "exec-once" => {
                let mut tokens = value.split_whitespace();
                if tokens.next().is_some_and(|t| t.ends_with("hyprctl"))
                    && tokens.next() == Some("setcursor")
                {
                    if let Some(theme) = tokens.next().filter(|_| cursor.theme.is_none()) {
                        cursor.theme = Some(theme.trim_matches('"').to_owned());
                    }
                    if cursor.size.is_none() {
                        cursor.size = tokens.next().and_then(|s| s.parse().ok());
                    }
                }
            }
            "source" => sources.push(value.to_owned()),
            _ => {}
        }
    }
    (cursor, sources)
}

/// Extracts `xcursor-theme` / `xcursor-size` from niri's KDL config.
// ponytail: line-oriented scan, not a KDL parser — enough for these two keys.
fn parse_niri(text: &str) -> Cursor {
    let mut cursor = Cursor::default();
    for line in text.lines() {
        let line = line.split("//").next().unwrap_or_default().trim();
        if let Some(rest) = line.strip_prefix("xcursor-theme") {
            let value = rest.trim().trim_matches('"');
            if cursor.theme.is_none() && !value.is_empty() {
                cursor.theme = Some(value.to_owned());
            }
        } else if let Some(rest) = line.strip_prefix("xcursor-size")
            && cursor.size.is_none()
        {
            cursor.size = rest.trim().parse().ok();
        }
    }
    cursor
}

/// Extracts `seat <name> xcursor_theme <theme> [size]` from a sway config
/// (`include` directives are not followed).
fn parse_sway(text: &str) -> Cursor {
    let mut cursor = Cursor::default();
    for line in text.lines() {
        let line = line.split('#').next().unwrap_or_default().trim();
        let mut tokens = line.split_whitespace();
        if tokens.next() != Some("seat") {
            continue;
        }
        let _seat_name = tokens.next();
        if tokens.next() != Some("xcursor_theme") {
            continue;
        }
        if let Some(theme) = tokens.next().filter(|_| cursor.theme.is_none()) {
            cursor.theme = Some(theme.trim_matches('"').to_owned());
        }
        if cursor.size.is_none() {
            cursor.size = tokens.next().and_then(|s| s.parse().ok());
        }
    }
    cursor
}

/// Extracts `gtk-cursor-theme-name` / `gtk-cursor-theme-size` from a GTK
/// `settings.ini`.
fn parse_gtk_settings(text: &str) -> Cursor {
    let mut cursor = Cursor::default();
    for line in text.lines() {
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        let value = value.trim().trim_matches('"');
        match key.trim() {
            "gtk-cursor-theme-name" if cursor.theme.is_none() && !value.is_empty() => {
                cursor.theme = Some(value.to_owned());
            }
            "gtk-cursor-theme-size" if cursor.size.is_none() => {
                cursor.size = value.parse().ok();
            }
            _ => {}
        }
    }
    cursor
}

/// Extracts the theme an `~/.icons/default/index.theme` inherits (the XDG
/// default-cursor mechanism; carries no size).
fn parse_index_theme(text: &str) -> Cursor {
    let theme = text
        .lines()
        .filter_map(|line| line.split_once('='))
        .find(|(key, _)| key.trim() == "Inherits")
        .and_then(|(_, value)| value.split(',').next())
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty());
    Cursor { theme, size: None }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hyprland_env_lines() {
        let (cursor, sources) = parse_hyprland(
            "monitor=,preferred,auto,1\n\
             env = XCURSOR_THEME,Bibata-Modern-Ice # my cursor\n\
             env=XCURSOR_SIZE,20\n\
             source = ~/.config/hypr/extra.conf\n",
        );
        assert_eq!(cursor.theme.as_deref(), Some("Bibata-Modern-Ice"));
        assert_eq!(cursor.size, Some(20));
        assert_eq!(sources, vec!["~/.config/hypr/extra.conf"]);
    }

    #[test]
    fn hyprland_setcursor_exec() {
        let (cursor, _) = parse_hyprland("exec-once = hyprctl setcursor Adwaita 32\n");
        assert_eq!(cursor.theme.as_deref(), Some("Adwaita"));
        assert_eq!(cursor.size, Some(32));
    }

    #[test]
    fn niri_cursor_block() {
        let cursor = parse_niri(
            "cursor {\n    xcursor-theme \"Bibata\" // comment\n    xcursor-size 28\n}\n",
        );
        assert_eq!(cursor.theme.as_deref(), Some("Bibata"));
        assert_eq!(cursor.size, Some(28));
    }

    #[test]
    fn sway_seat_line() {
        let cursor = parse_sway("seat * xcursor_theme \"Adwaita\" 48\n");
        assert_eq!(cursor.theme.as_deref(), Some("Adwaita"));
        assert_eq!(cursor.size, Some(48));
    }

    #[test]
    fn gtk_settings_ini() {
        let cursor = parse_gtk_settings(
            "[Settings]\ngtk-cursor-theme-name = Vimix\ngtk-cursor-theme-size=22\n",
        );
        assert_eq!(cursor.theme.as_deref(), Some("Vimix"));
        assert_eq!(cursor.size, Some(22));
    }

    #[test]
    fn index_theme_inherits() {
        let cursor = parse_index_theme("[Icon Theme]\nInherits=phinger-cursors-light, other\n");
        assert_eq!(cursor.theme.as_deref(), Some("phinger-cursors-light"));
        assert_eq!(cursor.size, None);
    }

    #[test]
    fn or_fills_missing_sides_only() {
        let a = Cursor {
            theme: Some("A".into()),
            size: None,
        };
        let b = Cursor {
            theme: Some("B".into()),
            size: Some(24),
        };
        let merged = a.or(b);
        assert_eq!(merged.theme.as_deref(), Some("A"));
        assert_eq!(merged.size, Some(24));
    }
}
