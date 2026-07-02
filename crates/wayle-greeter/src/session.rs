//! Session discovery + last-choice persistence for the greeter.
//!
//! A display-manager greeter lets the user pick which session to start.
//! Sessions are advertised as freedesktop `.desktop` files under
//! `wayland-sessions` and `xsessions` directories (the same ones sddm/gdm
//! read). We parse each for its display `Name` and `Exec` line, present them in
//! a dropdown, and remember the last pick in a small state file so it
//! pre-selects next time.
//!
//! X11 sessions are labelled `(X11)` and launched through `startx /usr/bin/env
//! <exec>` — greetd (unlike sddm) does not manage an X server, so `startx`
//! provides one (the tuigreet approach).

use std::{
    fs,
    path::{Path, PathBuf},
};

use tracing::warn;

/// Which kind of session a discovery directory advertises.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SessionKind {
    /// `wayland-sessions/*.desktop`: exec started directly by greetd.
    Wayland,
    /// `xsessions/*.desktop`: exec wrapped in `startx /usr/bin/env`.
    X11,
}

/// A selectable session parsed from a sessions `.desktop` file.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Session {
    /// Stable id: the `.desktop` file stem (e.g. `sway`), suffixed `-x11` for
    /// X11 sessions so both kinds can coexist. Used to remember the last
    /// choice across restarts.
    pub id: String,
    /// Human-readable name from `Name=` (falls back to the id), suffixed
    /// ` (X11)` for X11 sessions.
    pub name: String,
    /// Command argv from `Exec=`, with field codes stripped (and the `startx`
    /// wrapper prepended for X11 sessions).
    pub exec: Vec<String>,
}

/// Discovers `kind` sessions from every `*.desktop` under each dir in `dirs`.
///
/// Entries with `Hidden=true` or `NoDisplay=true` are skipped, as are files
/// with no usable `Exec`. Results are de-duplicated by id (earlier dirs win,
/// matching XDG precedence). Callers merging kinds sort the combined list.
#[must_use]
pub fn discover(dirs: &[PathBuf], kind: SessionKind) -> Vec<Session> {
    let mut sessions: Vec<Session> = Vec::new();
    for dir in dirs {
        let Ok(entries) = fs::read_dir(dir) else {
            continue; // a missing sessions dir is normal
        };
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().and_then(|e| e.to_str()) != Some("desktop") {
                continue;
            }
            let Some(stem) = path.file_stem().and_then(|s| s.to_str()) else {
                continue;
            };
            let id = match kind {
                SessionKind::Wayland => stem.to_owned(),
                SessionKind::X11 => format!("{stem}-x11"),
            };
            if sessions.iter().any(|s| s.id == id) {
                continue; // a higher-precedence dir already provided this id
            }
            // `None` = hidden, unreadable, or no Exec.
            if let Some(session) = parse_desktop(&fs::read_to_string(&path).unwrap_or_default(), id)
            {
                sessions.push(apply_kind(session, kind));
            }
        }
    }
    sessions
}

/// Applies the kind-specific labelling and launch wrapper.
fn apply_kind(mut session: Session, kind: SessionKind) -> Session {
    if kind == SessionKind::X11 {
        session.name.push_str(" (X11)");
        let mut exec = vec!["startx".to_owned(), "/usr/bin/env".to_owned()];
        exec.append(&mut session.exec);
        session.exec = exec;
    }
    session
}

/// Parses a desktop-entry `text`, returning `None` if it is hidden or lacks a
/// usable `Exec`.
fn parse_desktop(text: &str, id: String) -> Option<Session> {
    let mut name = None;
    let mut exec = None;
    let mut hidden = false;
    let mut in_entry = false;
    for line in text.lines() {
        let line = line.trim();
        if line.starts_with('[') {
            // Only read the [Desktop Entry] group; ignore action groups etc.
            in_entry = line == "[Desktop Entry]";
            continue;
        }
        if !in_entry {
            continue;
        }
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        let value = value.trim();
        match key.trim() {
            "Name" if name.is_none() => name = Some(value.to_owned()),
            "Exec" => exec = Some(value.to_owned()),
            "Hidden" | "NoDisplay" if value == "true" => hidden = true,
            _ => {}
        }
    }
    if hidden {
        return None;
    }
    let exec = exec.map(|e| parse_exec(&e)).filter(|v| !v.is_empty())?;
    let name = name.filter(|n| !n.is_empty()).unwrap_or_else(|| id.clone());
    Some(Session { id, name, exec })
}

/// Splits an `Exec=` line into argv.
///
/// ponytail: whitespace split with freedesktop field codes (`%f`, `%u`, …)
/// dropped. Full `Exec` quoting/escaping is not handled — session Exec lines are
/// near-universally simple (`sway`, `Hyprland`, `startplasma-wayland`). Upgrade
/// to a real parser only if a real session file needs quoted arguments.
fn parse_exec(exec: &str) -> Vec<String> {
    exec.split_whitespace()
        .filter(|tok| !(tok.len() == 2 && tok.starts_with('%')))
        .map(String::from)
        .collect()
}

/// Reads the remembered session id, or `None` if unset/unreadable.
#[must_use]
pub fn load_last(path: &Path) -> Option<String> {
    let id = fs::read_to_string(path).ok()?.trim().to_owned();
    (!id.is_empty()).then_some(id)
}

/// Persists `id` as the remembered session (best effort; logs on failure).
pub fn save_last(path: &Path, id: &str) {
    if let Some(parent) = path.parent() {
        let _ = fs::create_dir_all(parent);
    }
    if let Err(err) = fs::write(path, id) {
        warn!(path = %path.display(), %err, "greeter: could not save last session");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_name_and_exec() {
        let s = parse_desktop(
            "[Desktop Entry]\nName=Sway\nComment=tiling wm\nExec=sway\nType=Application\n",
            "sway".to_owned(),
        )
        .expect("session");
        assert_eq!(s.id, "sway");
        assert_eq!(s.name, "Sway");
        assert_eq!(s.exec, vec!["sway"]);
    }

    #[test]
    fn strips_field_codes_and_keeps_real_args() {
        let s = parse_desktop(
            "[Desktop Entry]\nName=Plasma\nExec=startplasma-wayland --foo %U\n",
            "plasma".to_owned(),
        )
        .expect("session");
        assert_eq!(s.exec, vec!["startplasma-wayland", "--foo"]);
    }

    #[test]
    fn hidden_and_nodisplay_are_skipped() {
        assert!(
            parse_desktop(
                "[Desktop Entry]\nName=X\nExec=x\nHidden=true\n",
                "x".to_owned()
            )
            .is_none()
        );
        assert!(
            parse_desktop(
                "[Desktop Entry]\nName=X\nExec=x\nNoDisplay=true\n",
                "x".to_owned()
            )
            .is_none()
        );
    }

    #[test]
    fn missing_exec_is_skipped() {
        assert!(parse_desktop("[Desktop Entry]\nName=X\n", "x".to_owned()).is_none());
    }

    #[test]
    fn name_defaults_to_id() {
        let s = parse_desktop("[Desktop Entry]\nExec=hyprland\n", "hyprland".to_owned())
            .expect("session");
        assert_eq!(s.name, "hyprland");
    }

    #[test]
    fn x11_sessions_are_labelled_and_wrapped() {
        let s = parse_desktop(
            "[Desktop Entry]\nName=Plasma\nExec=startplasma-x11\n",
            "plasma-x11".to_owned(),
        )
        .map(|s| apply_kind(s, SessionKind::X11))
        .expect("session");
        assert_eq!(s.id, "plasma-x11");
        assert_eq!(s.name, "Plasma (X11)");
        assert_eq!(s.exec, vec!["startx", "/usr/bin/env", "startplasma-x11"]);
    }

    #[test]
    fn wayland_sessions_are_untouched_by_kind() {
        let s = parse_desktop("[Desktop Entry]\nName=Sway\nExec=sway\n", "sway".to_owned())
            .map(|s| apply_kind(s, SessionKind::Wayland))
            .expect("session");
        assert_eq!(s.name, "Sway");
        assert_eq!(s.exec, vec!["sway"]);
    }

    #[test]
    fn ignores_keys_outside_desktop_entry_group() {
        // A `[Desktop Action]` group must not override the main Exec.
        let s = parse_desktop(
            "[Desktop Entry]\nName=Sway\nExec=sway\n[Desktop Action new]\nExec=sway -c other\n",
            "sway".to_owned(),
        )
        .expect("session");
        assert_eq!(s.exec, vec!["sway"]);
    }
}
