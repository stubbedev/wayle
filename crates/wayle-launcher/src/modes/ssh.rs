//! ssh mode: connect to known hosts in a terminal.

use std::{collections::BTreeSet, path::Path};

use async_trait::async_trait;
use tracing::warn;

use crate::{
    history::HistoryStore,
    item::Item,
    mode::{Action, ActivateKind, Mode, ModeState},
    spawn, template,
};

/// ssh mode knobs (mirrors rofi's `-ssh-*` family).
#[derive(Debug, Clone)]
pub struct SshConfig {
    /// SSH client binary.
    pub client: String,
    /// Connect template (`{terminal}`, `{ssh-client}`, `{host}`).
    pub command: String,
    /// Include hosts from `/etc/hosts`.
    pub parse_hosts: bool,
    /// Include hosts from `~/.ssh/known_hosts`.
    pub parse_known_hosts: bool,
    /// Terminal emulator ("" = autodetect).
    pub terminal: String,
    /// History cap.
    pub max_history: u32,
}

impl Default for SshConfig {
    fn default() -> Self {
        Self {
            client: "ssh".to_owned(),
            command: "{terminal} -e {ssh-client} {host}".to_owned(),
            parse_hosts: false,
            parse_known_hosts: true,
            terminal: String::new(),
            max_history: 25,
        }
    }
}

/// SSH host mode.
pub struct SshMode {
    config: SshConfig,
    history: Option<HistoryStore>,
    hosts: Vec<String>,
}

impl SshMode {
    /// Create the mode.
    pub fn new(config: SshConfig, history: Option<HistoryStore>) -> Self {
        Self {
            config,
            history,
            hosts: Vec::new(),
        }
    }

    fn connect(&self, host: &str) {
        let terminal = spawn::detect_terminal(&self.config.terminal);
        let command = template::render(&self.config.command, |key| match key {
            "host" => Some(host.to_owned()),
            "terminal" => Some(terminal.clone()),
            "ssh-client" => Some(self.config.client.clone()),
            _ => None,
        });
        spawn::run_shell(&command);
        if let Some(store) = &self.history
            && let Err(error) = store.record("ssh", host, self.config.max_history)
        {
            warn!(%error, "ssh history record failed");
        }
    }
}

#[async_trait]
impl Mode for SshMode {
    fn name(&self) -> &str {
        "ssh"
    }

    async fn load(&mut self) -> ModeState {
        let home = std::env::var("HOME").unwrap_or_default();
        let mut hosts = BTreeSet::new();
        parse_ssh_config(Path::new(&home).join(".ssh/config"), &mut hosts);
        if self.config.parse_known_hosts {
            parse_known_hosts(Path::new(&home).join(".ssh/known_hosts"), &mut hosts);
        }
        if self.config.parse_hosts {
            parse_etc_hosts(Path::new("/etc/hosts"), &mut hosts);
        }

        // Recently used first, alphabetical rest (rofi ssh ordering).
        let recent = self
            .history
            .as_ref()
            .and_then(|store| store.recent("ssh").ok())
            .unwrap_or_default();
        let mut ordered: Vec<String> = recent
            .iter()
            .filter(|host| hosts.contains(*host))
            .cloned()
            .collect();
        ordered.extend(hosts.iter().filter(|host| !recent.contains(host)).cloned());
        self.hosts = ordered;

        ModeState {
            items: self.hosts.iter().map(Item::new).collect(),
            prompt: "ssh".to_owned(),
            ..ModeState::default()
        }
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, _input: &str) -> Action {
        let host = match (&index, &kind) {
            (Some(row), _) => match self.hosts.get(*row as usize) {
                Some(host) => host.clone(),
                None => return Action::Nothing,
            },
            (None, ActivateKind::Custom(input)) => input.clone(),
            (None, _) => return Action::Nothing,
        };
        if host.trim().is_empty() {
            return Action::Nothing;
        }
        self.connect(host.trim());
        Action::Close
    }

    async fn delete(&mut self, index: u32) -> Action {
        if let (Some(store), Some(host)) = (&self.history, self.hosts.get(index as usize)) {
            if let Err(error) = store.remove("ssh", host) {
                warn!(%error, "ssh history delete failed");
            }
            return Action::Reload(self.load().await);
        }
        Action::Nothing
    }
}

/// `Host` entries from an ssh config, skipping wildcard/negated patterns.
/// `Include` directives are followed one level deep.
fn parse_ssh_config(path: impl AsRef<Path>, hosts: &mut BTreeSet<String>) {
    let Ok(content) = std::fs::read_to_string(path.as_ref()) else {
        return;
    };
    for line in content.lines() {
        let line = line.trim();
        let Some((keyword, rest)) = line.split_once(char::is_whitespace) else {
            continue;
        };
        match keyword.to_ascii_lowercase().as_str() {
            "host" => {
                for host in rest.split_whitespace() {
                    if !host.contains(['*', '?']) && !host.starts_with('!') {
                        hosts.insert(host.to_owned());
                    }
                }
            }
            "include" => {
                for pattern in rest.split_whitespace() {
                    let pattern = if pattern.starts_with('/') || pattern.starts_with('~') {
                        pattern.replace('~', &std::env::var("HOME").unwrap_or_default())
                    } else {
                        // Relative includes resolve against ~/.ssh.
                        format!(
                            "{}/.ssh/{pattern}",
                            std::env::var("HOME").unwrap_or_default()
                        )
                    };
                    if let Ok(paths) = glob::glob(&pattern) {
                        for included in paths.flatten() {
                            parse_ssh_config_flat(&included, hosts);
                        }
                    }
                }
            }
            _ => {}
        }
    }
}

/// One-level include: parses `Host` lines only (no nested includes).
fn parse_ssh_config_flat(path: &Path, hosts: &mut BTreeSet<String>) {
    let Ok(content) = std::fs::read_to_string(path) else {
        return;
    };
    for line in content.lines() {
        let line = line.trim();
        if let Some((keyword, rest)) = line.split_once(char::is_whitespace)
            && keyword.eq_ignore_ascii_case("host")
        {
            for host in rest.split_whitespace() {
                if !host.contains(['*', '?']) && !host.starts_with('!') {
                    hosts.insert(host.to_owned());
                }
            }
        }
    }
}

/// Hostnames from `known_hosts` (first field; hashed entries skipped,
/// brackets/ports stripped, comma lists split).
fn parse_known_hosts(path: impl AsRef<Path>, hosts: &mut BTreeSet<String>) {
    let Ok(content) = std::fs::read_to_string(path.as_ref()) else {
        return;
    };
    for line in content.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') || line.starts_with('|') {
            continue;
        }
        let Some(first) = line.split_whitespace().next() else {
            continue;
        };
        for entry in first.split(',') {
            let host = entry
                .trim_start_matches('[')
                .split(']')
                .next()
                .unwrap_or(entry);
            let host = host.split(':').next().unwrap_or(host);
            if !host.is_empty() {
                hosts.insert(host.to_owned());
            }
        }
    }
}

/// Hostnames from `/etc/hosts` (all names after the address; comments and
/// localhost variants skipped).
fn parse_etc_hosts(path: impl AsRef<Path>, hosts: &mut BTreeSet<String>) {
    let Ok(content) = std::fs::read_to_string(path.as_ref()) else {
        return;
    };
    for line in content.lines() {
        let line = line.split('#').next().unwrap_or("").trim();
        let mut fields = line.split_whitespace();
        let Some(_address) = fields.next() else {
            continue;
        };
        for name in fields {
            if !matches!(name, "localhost" | "localhost.localdomain" | "ip6-localhost")
                && !name.starts_with("ip6-")
            {
                hosts.insert(name.to_owned());
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_file(name: &str, content: &str) -> std::path::PathBuf {
        let dir = std::env::temp_dir().join(format!("wayle-ssh-test-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join(name);
        std::fs::write(&path, content).unwrap();
        path
    }

    #[test]
    fn ssh_config_hosts_skip_wildcards() {
        let path = temp_file(
            "config",
            "Host github.com\n  User git\nHost *.internal !bad prod-1 prod-2\nHost ?\n",
        );
        let mut hosts = BTreeSet::new();
        parse_ssh_config(&path, &mut hosts);
        assert_eq!(
            hosts.into_iter().collect::<Vec<_>>(),
            vec!["github.com", "prod-1", "prod-2"]
        );
    }

    #[test]
    fn known_hosts_strip_ports_and_hashes() {
        let path = temp_file(
            "known_hosts",
            "github.com ssh-ed25519 AAAA\n[bastion.example.com]:2222,10.0.0.1 ecdsa AAAA\n|1|hashed|entry ssh-rsa AAAA\n",
        );
        let mut hosts = BTreeSet::new();
        parse_known_hosts(&path, &mut hosts);
        let hosts: Vec<_> = hosts.into_iter().collect();
        assert_eq!(hosts, vec!["10.0.0.1", "bastion.example.com", "github.com"]);
    }

    #[test]
    fn etc_hosts_skips_localhost() {
        let path = temp_file(
            "hosts",
            "127.0.0.1 localhost\n::1 ip6-localhost ip6-loopback\n10.0.0.5 nas nas.lan # media box\n",
        );
        let mut hosts = BTreeSet::new();
        parse_etc_hosts(&path, &mut hosts);
        let hosts: Vec<_> = hosts.into_iter().collect();
        assert_eq!(hosts, vec!["nas", "nas.lan"]);
    }
}
