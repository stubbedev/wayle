//! run mode: execute commands found in `$PATH`.

use std::{collections::BTreeSet, os::unix::fs::PermissionsExt, path::Path};

use async_trait::async_trait;
use tracing::warn;

use crate::{
    history::HistoryStore,
    item::Item,
    mode::{Action, ActivateKind, Mode, ModeState},
    spawn, template,
};

/// run behavior knobs (mirrors rofi's `-run-*` family).
#[derive(Debug, Clone)]
pub struct RunConfig {
    /// Plain accept template (rofi `-run-command`, `{cmd}`).
    pub run_command: String,
    /// Alt accept template (rofi `-run-shell-command`,
    /// `{terminal}`/`{cmd}`).
    pub shell_command: String,
    /// Extra command whose stdout lines add entries (rofi
    /// `-run-list-command`).
    pub list_command: String,
    /// Terminal emulator ("" = autodetect).
    pub terminal: String,
    /// History cap.
    pub max_history: u32,
}

impl Default for RunConfig {
    fn default() -> Self {
        Self {
            run_command: "{cmd}".to_owned(),
            shell_command: "{terminal} -e {cmd}".to_owned(),
            list_command: String::new(),
            terminal: String::new(),
            max_history: 25,
        }
    }
}

/// Command runner mode over `$PATH` executables.
pub struct RunMode {
    config: RunConfig,
    history: Option<HistoryStore>,
    commands: Vec<String>,
}

impl RunMode {
    /// Create the mode. `history` enables recency ordering + recording.
    pub fn new(config: RunConfig, history: Option<HistoryStore>) -> Self {
        Self {
            config,
            history,
            commands: Vec::new(),
        }
    }

    fn execute(&self, command: &str, in_terminal: bool) {
        let rendered = if in_terminal {
            let terminal = spawn::detect_terminal(&self.config.terminal);
            template::render(&self.config.shell_command, |key| match key {
                "cmd" => Some(command.to_owned()),
                "terminal" => Some(terminal.clone()),
                _ => None,
            })
        } else {
            template::render(&self.config.run_command, |key| match key {
                "cmd" => Some(command.to_owned()),
                _ => None,
            })
        };
        spawn::run_shell(&rendered);
        if let Some(store) = &self.history
            && let Err(error) = store.record("run", command, self.config.max_history)
        {
            warn!(%error, "run history record failed");
        }
    }
}

#[async_trait]
impl Mode for RunMode {
    fn name(&self) -> &str {
        "run"
    }

    async fn load(&mut self) -> ModeState {
        let mut names = scan_path();
        if !self.config.list_command.trim().is_empty() {
            names.extend(list_command_entries(&self.config.list_command).await);
        }
        let recent = self
            .history
            .as_ref()
            .and_then(|store| store.recent("run").ok())
            .unwrap_or_default();
        self.commands = order_by_recent(names, &recent);
        ModeState {
            items: self.commands.iter().map(Item::new).collect(),
            prompt: "run".to_owned(),
            ..ModeState::default()
        }
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, _input: &str) -> Action {
        let command = match (&index, &kind) {
            (Some(row), _) => match self.commands.get(*row as usize) {
                Some(command) => command.clone(),
                None => return Action::Nothing,
            },
            (None, ActivateKind::Custom(input)) => input.clone(),
            (None, _) => return Action::Nothing,
        };
        if command.trim().is_empty() {
            return Action::Nothing;
        }
        self.execute(&command, matches!(kind, ActivateKind::Alt));
        Action::Close
    }

    async fn delete(&mut self, index: u32) -> Action {
        if let (Some(store), Some(command)) = (&self.history, self.commands.get(index as usize)) {
            if let Err(error) = store.remove("run", command) {
                warn!(%error, "run history delete failed");
            }
            return Action::Reload(self.load().await);
        }
        Action::Nothing
    }
}

/// Recently used first (rofi run ordering), then the alphabetical rest.
fn order_by_recent(names: BTreeSet<String>, recent: &[String]) -> Vec<String> {
    let mut ordered: Vec<String> = recent
        .iter()
        .filter(|entry| names.contains(*entry))
        .cloned()
        .collect();
    ordered.extend(names.iter().filter(|name| !recent.contains(name)).cloned());
    ordered
}

/// Executable file names on `$PATH`, sorted, deduped.
fn scan_path() -> BTreeSet<String> {
    let mut names = BTreeSet::new();
    let Some(path) = std::env::var_os("PATH") else {
        return names;
    };
    for dir in std::env::split_paths(&path) {
        collect_executables(&dir, &mut names);
    }
    names
}

fn collect_executables(dir: &Path, names: &mut BTreeSet<String>) {
    let Ok(entries) = std::fs::read_dir(dir) else {
        return;
    };
    for entry in entries.flatten() {
        let Ok(metadata) = entry.metadata() else {
            continue;
        };
        if !metadata.is_file() || metadata.permissions().mode() & 0o111 == 0 {
            continue;
        }
        if let Some(name) = entry.file_name().to_str() {
            names.insert(name.to_owned());
        }
    }
}

async fn list_command_entries(command: &str) -> Vec<String> {
    let output = tokio::process::Command::new("sh")
        .arg("-c")
        .arg(command)
        .output()
        .await;
    match output {
        Ok(output) => String::from_utf8_lossy(&output.stdout)
            .lines()
            .filter(|line| !line.is_empty())
            .map(ToOwned::to_owned)
            .collect(),
        Err(error) => {
            warn!(%command, %error, "run-list-command failed");
            Vec::new()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scan_finds_only_executables() {
        let dir = std::env::temp_dir().join(format!("wayle-run-test-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        for (name, mode) in [("mytool", 0o755), ("notexec", 0o644)] {
            let path = dir.join(name);
            std::fs::write(&path, "#!/bin/sh\n").unwrap();
            std::fs::set_permissions(&path, std::fs::Permissions::from_mode(mode)).unwrap();
        }
        let mut names = BTreeSet::new();
        collect_executables(&dir, &mut names);
        assert!(names.contains("mytool"));
        assert!(!names.contains("notexec"));
        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn recent_commands_order_first() {
        let names: BTreeSet<String> = ["alpha", "mytool", "zeta"]
            .into_iter()
            .map(ToOwned::to_owned)
            .collect();
        let recent = vec!["mytool".to_owned(), "gone".to_owned()];
        assert_eq!(
            order_by_recent(names, &recent),
            vec!["mytool", "alpha", "zeta"]
        );
    }
}
