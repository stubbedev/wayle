//! filebrowser / recursivebrowser modes.

use std::{
    fs,
    path::{Path, PathBuf},
    time::SystemTime,
};

use async_trait::async_trait;

use crate::{
    item::{IconSource, Item},
    mode::{Action, ActivateKind, Mode, ModeState},
    spawn,
};

/// Cap on collected entries (the recursive walk of a home directory is
/// unbounded otherwise).
// ponytail: hard cap + skip; stream into the matcher injector if someone
// actually needs full-disk recursion.
const MAX_ENTRIES: usize = 50_000;

/// File ordering (rofi filebrowser `sorting-method`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum FileSort {
    /// By name.
    #[default]
    Name,
    /// By modification time (newest first).
    Mtime,
    /// By access time.
    Atime,
    /// By creation time.
    Ctime,
}

/// filebrowser knobs.
#[derive(Debug, Clone)]
pub struct FileBrowserConfig {
    /// Start directory ("" = home).
    pub directory: String,
    /// File ordering.
    pub sorting: FileSort,
    /// Directories before files.
    pub directories_first: bool,
    /// Show dotfiles.
    pub show_hidden: bool,
    /// Command opening a picked file ("" = xdg-open).
    pub command: String,
    /// Recursive listing (recursivebrowser).
    pub recursive: bool,
}

impl Default for FileBrowserConfig {
    fn default() -> Self {
        Self {
            directory: String::new(),
            sorting: FileSort::Name,
            directories_first: true,
            show_hidden: false,
            command: String::new(),
            recursive: false,
        }
    }
}

enum Entry {
    Parent(PathBuf),
    Dir(PathBuf),
    File(PathBuf),
}

/// File browser mode.
pub struct FileBrowserMode {
    config: FileBrowserConfig,
    current: PathBuf,
    entries: Vec<Entry>,
}

impl FileBrowserMode {
    /// Create the mode rooted at the configured directory.
    #[must_use]
    pub fn new(config: FileBrowserConfig) -> Self {
        let current = if config.directory.is_empty() {
            std::env::var("HOME").map(PathBuf::from).unwrap_or_default()
        } else {
            PathBuf::from(shellexpand_home(&config.directory))
        };
        Self {
            config,
            current,
            entries: Vec::new(),
        }
    }

    fn state(&mut self) -> ModeState {
        let mut listed = if self.config.recursive {
            walk(&self.current, self.config.show_hidden)
        } else {
            list_dir(&self.current, self.config.show_hidden)
        };
        sort_entries(&mut listed, self.config.sorting, self.config.directories_first);

        let mut entries = Vec::with_capacity(listed.len() + 1);
        let mut items = Vec::with_capacity(listed.len() + 1);
        if !self.config.recursive
            && let Some(parent) = self.current.parent()
        {
            entries.push(Entry::Parent(parent.to_path_buf()));
            items.push(Item {
                display: "..".to_owned(),
                match_text: "..".to_owned(),
                icon: Some(IconSource::Name("folder-symbolic".to_owned())),
                info: None,
                flags: crate::item::ItemFlags::empty(),
            });
        }
        for (path, is_dir) in listed {
            items.push(entry_item(&path, is_dir, &self.current, self.config.recursive));
            entries.push(if is_dir { Entry::Dir(path) } else { Entry::File(path) });
        }
        self.entries = entries;

        ModeState {
            items,
            prompt: display_dir(&self.current),
            no_custom: false,
            ..ModeState::default()
        }
    }

    fn open_file(&self, path: &Path) {
        let opener = if self.config.command.trim().is_empty() {
            "xdg-open"
        } else {
            self.config.command.trim()
        };
        let quoted = shlex::try_quote(&path.display().to_string())
            .map(|quoted| quoted.into_owned())
            .unwrap_or_else(|_| path.display().to_string());
        spawn::run_shell(&format!("{opener} {quoted}"));
    }
}

#[async_trait]
impl Mode for FileBrowserMode {
    fn name(&self) -> &str {
        if self.config.recursive {
            "recursivebrowser"
        } else {
            "filebrowser"
        }
    }

    async fn load(&mut self) -> ModeState {
        self.state()
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, _input: &str) -> Action {
        match index.and_then(|row| self.entries.get(row as usize)) {
            Some(Entry::Parent(path) | Entry::Dir(path)) => {
                self.current = path.clone();
                Action::Reload(self.state())
            }
            Some(Entry::File(path)) => {
                self.open_file(&path.clone());
                Action::Close
            }
            None => {
                // Custom input: navigate to a typed path, or open it.
                let ActivateKind::Custom(input) = kind else {
                    return Action::Nothing;
                };
                let path = PathBuf::from(shellexpand_home(input.trim()));
                if path.is_dir() {
                    self.current = path;
                    Action::Reload(self.state())
                } else if path.is_file() {
                    self.open_file(&path);
                    Action::Close
                } else {
                    Action::Nothing
                }
            }
        }
    }
}

fn shellexpand_home(path: &str) -> String {
    match path.strip_prefix("~") {
        Some(rest) => format!("{}{rest}", std::env::var("HOME").unwrap_or_default()),
        None => path.to_owned(),
    }
}

fn display_dir(path: &Path) -> String {
    let display = path.display().to_string();
    match std::env::var("HOME") {
        Ok(home) if display.starts_with(&home) => display.replacen(&home, "~", 1),
        _ => display,
    }
}

fn list_dir(dir: &Path, show_hidden: bool) -> Vec<(PathBuf, bool)> {
    let Ok(read) = fs::read_dir(dir) else {
        return Vec::new();
    };
    read.flatten()
        .filter(|entry| show_hidden || !is_hidden(&entry.path()))
        .take(MAX_ENTRIES)
        .map(|entry| {
            let path = entry.path();
            let is_dir = path.is_dir();
            (path, is_dir)
        })
        .collect()
}

/// Breadth-first recursive walk, capped at [`MAX_ENTRIES`].
fn walk(root: &Path, show_hidden: bool) -> Vec<(PathBuf, bool)> {
    let mut out = Vec::new();
    let mut queue = std::collections::VecDeque::from([root.to_path_buf()]);
    while let Some(dir) = queue.pop_front() {
        let Ok(read) = fs::read_dir(&dir) else {
            continue;
        };
        for entry in read.flatten() {
            let path = entry.path();
            if !show_hidden && is_hidden(&path) {
                continue;
            }
            if path.is_symlink() {
                continue; // avoid cycles
            }
            let is_dir = path.is_dir();
            if is_dir {
                queue.push_back(path.clone());
            } else {
                out.push((path, false));
            }
            if out.len() >= MAX_ENTRIES {
                tracing::warn!(
                    limit = MAX_ENTRIES,
                    "recursive browser hit the entry cap; listing truncated"
                );
                return out;
            }
        }
    }
    out
}

fn is_hidden(path: &Path) -> bool {
    path.file_name()
        .and_then(|name| name.to_str())
        .is_some_and(|name| name.starts_with('.'))
}

fn sort_entries(entries: &mut [(PathBuf, bool)], sorting: FileSort, directories_first: bool) {
    let time_key = |path: &Path| -> SystemTime {
        let metadata = path.metadata().ok();
        let time = match sorting {
            FileSort::Mtime => metadata.and_then(|m| m.modified().ok()),
            FileSort::Atime => metadata.and_then(|m| m.accessed().ok()),
            FileSort::Ctime => metadata.and_then(|m| m.created().ok()),
            FileSort::Name => None,
        };
        time.unwrap_or(SystemTime::UNIX_EPOCH)
    };
    entries.sort_by(|(path_a, dir_a), (path_b, dir_b)| {
        let group = if directories_first {
            dir_b.cmp(dir_a)
        } else {
            std::cmp::Ordering::Equal
        };
        group.then_with(|| match sorting {
            FileSort::Name => path_a
                .file_name()
                .map(|n| n.to_ascii_lowercase())
                .cmp(&path_b.file_name().map(|n| n.to_ascii_lowercase())),
            _ => time_key(path_b).cmp(&time_key(path_a)),
        })
    });
}

fn entry_item(path: &Path, is_dir: bool, base: &Path, recursive: bool) -> Item {
    let name = if recursive {
        path.strip_prefix(base)
            .unwrap_or(path)
            .display()
            .to_string()
    } else {
        path.file_name()
            .map(|name| name.to_string_lossy().into_owned())
            .unwrap_or_else(|| path.display().to_string())
    };
    let icon = if is_dir {
        "folder-symbolic".to_owned()
    } else {
        file_icon(path)
    };
    Item {
        match_text: name.clone(),
        display: name,
        icon: Some(IconSource::Name(icon)),
        info: None,
        flags: crate::item::ItemFlags::empty(),
    }
}

/// Icon name from the file's guessed content type.
fn file_icon(path: &Path) -> String {
    let (content_type, _uncertain) = gio::functions::content_type_guess(Some(path), None);
    let icon = gio::functions::content_type_get_generic_icon_name(&content_type);
    icon.map_or_else(
        || "text-x-generic-symbolic".to_owned(),
        |name| name.to_string(),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> PathBuf {
        let dir = std::env::temp_dir().join(format!("wayle-fb-test-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(dir.join("sub")).unwrap();
        std::fs::write(dir.join("b.txt"), "b").unwrap();
        std::fs::write(dir.join("a.txt"), "a").unwrap();
        std::fs::write(dir.join(".hidden"), "h").unwrap();
        std::fs::write(dir.join("sub/deep.txt"), "d").unwrap();
        dir
    }

    #[test]
    fn listing_sorts_dirs_first_and_hides_dotfiles() {
        let dir = fixture();
        let mut entries = list_dir(&dir, false);
        sort_entries(&mut entries, FileSort::Name, true);
        let names: Vec<String> = entries
            .iter()
            .map(|(path, _)| path.file_name().unwrap().to_string_lossy().into_owned())
            .collect();
        assert_eq!(names, vec!["sub", "a.txt", "b.txt"]);
        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn recursive_walk_finds_nested_files() {
        let dir = fixture();
        let entries = walk(&dir, false);
        assert!(
            entries
                .iter()
                .any(|(path, _)| path.ends_with("sub/deep.txt"))
        );
        assert!(entries.iter().all(|(_, is_dir)| !is_dir));
        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn hidden_files_shown_when_enabled() {
        let dir = fixture();
        let entries = list_dir(&dir, true);
        assert!(entries.iter().any(|(path, _)| path.ends_with(".hidden")));
        std::fs::remove_dir_all(&dir).ok();
    }
}
