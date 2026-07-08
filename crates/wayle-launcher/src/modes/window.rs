//! window / windowcd modes: switch between open windows.
//!
//! Talks to the compositor through its own CLI (`hyprctl` / `niri msg` /
//! `swaymsg`) — one JSON query per session open, no persistent IPC.
// ponytail: CLI exec is ~5-10ms per open; switch to native sockets if that
// ever shows up in the open latency budget.

use async_trait::async_trait;
use tracing::warn;

use crate::{
    item::Item,
    mode::{Action, ActivateKind, Mode, ModeState},
    spawn, template,
};

/// Window fields searchable via `window-match-fields`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WindowField {
    /// Window title.
    Title,
    /// Application class / app-id.
    Class,
    /// Window name.
    Name,
    /// Window role.
    Role,
    /// Workspace name.
    Desktop,
}

/// window mode knobs (mirrors rofi's `-window-*` family).
#[derive(Debug, Clone)]
pub struct WindowConfig {
    /// Row template: `{w}` workspace, `{c}` class, `{t}` title, `{n}` name,
    /// `{r}` role.
    pub format: String,
    /// Fields fed to the matcher.
    pub match_fields: Vec<WindowField>,
    /// Hide the currently focused window.
    pub hide_active: bool,
    /// Shift-delete closes the window.
    pub close_on_delete: bool,
    /// Alt-accept command with `{window}` substituted (rofi
    /// `-window-command`).
    pub window_command: String,
    /// Restrict to the current workspace (windowcd).
    pub current_desktop_only: bool,
}

impl Default for WindowConfig {
    fn default() -> Self {
        Self {
            format: "{w}   {c}   {t}".to_owned(),
            match_fields: vec![WindowField::Title, WindowField::Class],
            hide_active: false,
            close_on_delete: true,
            window_command: String::new(),
            current_desktop_only: false,
        }
    }
}

/// One open window, compositor-agnostic.
#[derive(Debug, Clone)]
struct WindowInfo {
    /// Opaque compositor id (hyprland address / niri id / sway con_id).
    id: String,
    title: String,
    class: String,
    workspace: String,
    focused: bool,
    /// Lower = more recently focused (when the compositor reports it).
    focus_order: i64,
    on_current_workspace: bool,
}

/// Which compositor CLI to talk to.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Backend {
    Hyprland,
    Niri,
    Sway,
}

impl Backend {
    fn detect() -> Option<Self> {
        if std::env::var_os("HYPRLAND_INSTANCE_SIGNATURE").is_some() {
            Some(Self::Hyprland)
        } else if std::env::var_os("NIRI_SOCKET").is_some() {
            Some(Self::Niri)
        } else if std::env::var_os("SWAYSOCK").is_some() {
            Some(Self::Sway)
        } else {
            None
        }
    }
}

/// Window switcher mode.
pub struct WindowMode {
    config: WindowConfig,
    backend: Option<Backend>,
    windows: Vec<WindowInfo>,
}

impl WindowMode {
    /// Create the mode; the compositor is detected from the environment.
    #[must_use]
    pub fn new(config: WindowConfig) -> Self {
        Self {
            config,
            backend: Backend::detect(),
            windows: Vec::new(),
        }
    }

    fn mode_name(&self) -> &'static str {
        if self.config.current_desktop_only {
            "windowcd"
        } else {
            "window"
        }
    }
}

#[async_trait]
impl Mode for WindowMode {
    fn name(&self) -> &str {
        self.mode_name()
    }

    async fn load(&mut self) -> ModeState {
        let mut windows = match self.backend {
            Some(backend) => list_windows(backend).await,
            None => {
                warn!("window mode: no supported compositor detected");
                Vec::new()
            }
        };
        if self.config.hide_active {
            windows.retain(|window| !window.focused);
        }
        if self.config.current_desktop_only {
            windows.retain(|window| window.on_current_workspace);
        }
        // MRU: most recently focused first (the focused window itself sorts
        // last so plain Enter switches to the previous window, like rofi).
        windows.sort_by_key(|window| (window.focused, window.focus_order));

        let items = windows
            .iter()
            .map(|window| window_item(window, &self.config))
            .collect();
        self.windows = windows;
        ModeState {
            items,
            prompt: self.mode_name().to_owned(),
            no_custom: true,
            ..ModeState::default()
        }
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, _input: &str) -> Action {
        let (Some(backend), Some(window)) = (
            self.backend,
            index.and_then(|row| self.windows.get(row as usize)),
        ) else {
            return Action::Nothing;
        };
        if matches!(kind, ActivateKind::Alt) && !self.config.window_command.is_empty() {
            let command = template::render(&self.config.window_command, |key| match key {
                "window" => Some(window.id.clone()),
                _ => None,
            });
            spawn::run_shell(&command);
            return Action::Close;
        }
        focus_window(backend, &window.id).await;
        Action::Close
    }

    async fn delete(&mut self, index: u32) -> Action {
        if !self.config.close_on_delete {
            return Action::Nothing;
        }
        if let (Some(backend), Some(window)) = (self.backend, self.windows.get(index as usize)) {
            close_window(backend, &window.id).await;
            return Action::Reload(self.load().await);
        }
        Action::Nothing
    }

    fn allows_custom(&self) -> bool {
        false
    }
}

fn window_item(window: &WindowInfo, config: &WindowConfig) -> Item {
    let display = template::render(&config.format, |key| match key {
        "w" => Some(window.workspace.clone()),
        "c" => Some(window.class.clone()),
        "t" => Some(window.title.clone()),
        "n" => Some(window.class.clone()),
        "r" => Some(String::new()),
        _ => None,
    });
    let match_text = config
        .match_fields
        .iter()
        .map(|field| match field {
            WindowField::Title => window.title.as_str(),
            WindowField::Class | WindowField::Name | WindowField::Role => window.class.as_str(),
            WindowField::Desktop => window.workspace.as_str(),
        })
        .collect::<Vec<_>>()
        .join(" ");
    Item {
        display,
        match_text,
        icon: Some(crate::item::IconSource::Name(window.class.to_lowercase())),
        info: Some(window.id.clone()),
        flags: crate::item::ItemFlags::empty(),
    }
}

/// Run a CLI and parse its stdout as JSON.
async fn json_command(program: &str, args: &[&str]) -> Option<serde_json::Value> {
    let output = tokio::process::Command::new(program)
        .args(args)
        .output()
        .await;
    match output {
        Ok(output) if output.status.success() => serde_json::from_slice(&output.stdout).ok(),
        Ok(output) => {
            warn!(%program, status = %output.status, "compositor query failed");
            None
        }
        Err(error) => {
            warn!(%program, %error, "compositor CLI unavailable");
            None
        }
    }
}

async fn list_windows(backend: Backend) -> Vec<WindowInfo> {
    match backend {
        Backend::Hyprland => hyprland_windows().await,
        Backend::Niri => niri_windows().await,
        Backend::Sway => sway_windows().await,
    }
}

async fn focus_window(backend: Backend, id: &str) {
    match backend {
        Backend::Hyprland => {
            spawn::run_argv(&argv(&[
                "hyprctl",
                "dispatch",
                "focuswindow",
                &format!("address:{id}"),
            ]));
        }
        Backend::Niri => {
            spawn::run_argv(&argv(&[
                "niri",
                "msg",
                "action",
                "focus-window",
                "--id",
                id,
            ]));
        }
        Backend::Sway => {
            spawn::run_argv(&argv(&["swaymsg", &format!("[con_id={id}] focus")]));
        }
    }
}

async fn close_window(backend: Backend, id: &str) {
    match backend {
        Backend::Hyprland => {
            spawn::run_argv(&argv(&[
                "hyprctl",
                "dispatch",
                "closewindow",
                &format!("address:{id}"),
            ]));
        }
        Backend::Niri => {
            spawn::run_argv(&argv(&[
                "niri",
                "msg",
                "action",
                "close-window",
                "--id",
                id,
            ]));
        }
        Backend::Sway => {
            spawn::run_argv(&argv(&["swaymsg", &format!("[con_id={id}] kill")]));
        }
    }
    // Give the compositor a beat before the reload re-queries the list.
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;
}

fn argv(parts: &[&str]) -> Vec<String> {
    parts.iter().map(ToString::to_string).collect()
}

async fn hyprland_windows() -> Vec<WindowInfo> {
    let Some(clients) = json_command("hyprctl", &["-j", "clients"]).await else {
        return Vec::new();
    };
    let active_workspace = json_command("hyprctl", &["-j", "activeworkspace"])
        .await
        .and_then(|workspace| workspace.get("id").and_then(serde_json::Value::as_i64));
    let Some(clients) = clients.as_array() else {
        return Vec::new();
    };
    clients
        .iter()
        .filter(|client| client["mapped"].as_bool().unwrap_or(true))
        .map(|client| {
            let workspace_id = client["workspace"]["id"].as_i64();
            WindowInfo {
                id: client["address"].as_str().unwrap_or_default().to_owned(),
                title: client["title"].as_str().unwrap_or_default().to_owned(),
                class: client["class"].as_str().unwrap_or_default().to_owned(),
                workspace: client["workspace"]["name"]
                    .as_str()
                    .unwrap_or_default()
                    .to_owned(),
                focused: client["focusHistoryID"].as_i64() == Some(0),
                focus_order: client["focusHistoryID"].as_i64().unwrap_or(i64::MAX),
                on_current_workspace: workspace_id.is_some() && workspace_id == active_workspace,
            }
        })
        .collect()
}

async fn niri_windows() -> Vec<WindowInfo> {
    let Some(windows) = json_command("niri", &["msg", "-j", "windows"]).await else {
        return Vec::new();
    };
    let Some(windows) = windows.as_array() else {
        return Vec::new();
    };
    let focused_workspace = windows
        .iter()
        .find(|window| window["is_focused"].as_bool() == Some(true))
        .and_then(|window| window["workspace_id"].as_i64());
    windows
        .iter()
        .enumerate()
        .map(|(order, window)| WindowInfo {
            id: window["id"].as_i64().unwrap_or_default().to_string(),
            title: window["title"].as_str().unwrap_or_default().to_owned(),
            class: window["app_id"].as_str().unwrap_or_default().to_owned(),
            workspace: window["workspace_id"]
                .as_i64()
                .map(|id| id.to_string())
                .unwrap_or_default(),
            focused: window["is_focused"].as_bool() == Some(true),
            focus_order: i64::try_from(order).unwrap_or(i64::MAX), // niri msg lacks MRU
            on_current_workspace: window["workspace_id"].as_i64() == focused_workspace,
        })
        .collect()
}

async fn sway_windows() -> Vec<WindowInfo> {
    let Some(tree) = json_command("swaymsg", &["-t", "get_tree", "-r"]).await else {
        return Vec::new();
    };
    let mut windows = Vec::new();
    walk_sway(&tree, None, &mut windows);
    let focused_workspace = windows
        .iter()
        .find(|window| window.focused)
        .map(|window| window.workspace.clone());
    for window in &mut windows {
        window.on_current_workspace = focused_workspace.as_deref() == Some(&window.workspace);
    }
    windows
}

/// Recursively collect view nodes, carrying the nearest workspace name.
fn walk_sway(node: &serde_json::Value, workspace: Option<&str>, out: &mut Vec<WindowInfo>) {
    let node_workspace = if node["type"].as_str() == Some("workspace") {
        node["name"].as_str()
    } else {
        workspace
    };
    let is_view = node["type"]
        .as_str()
        .is_some_and(|t| t == "con" || t == "floating_con")
        && (node["app_id"].is_string() || node["window_properties"].is_object())
        && node["name"].is_string()
        && node["nodes"].as_array().is_none_or(Vec::is_empty);
    if is_view {
        out.push(WindowInfo {
            id: node["id"].as_i64().unwrap_or_default().to_string(),
            title: node["name"].as_str().unwrap_or_default().to_owned(),
            class: node["app_id"]
                .as_str()
                .or_else(|| node["window_properties"]["class"].as_str())
                .unwrap_or_default()
                .to_owned(),
            workspace: node_workspace.unwrap_or_default().to_owned(),
            focused: node["focused"].as_bool() == Some(true),
            focus_order: i64::MAX,       // sway tree lacks MRU
            on_current_workspace: false, // filled in by sway_windows
        });
    }
    for child_key in ["nodes", "floating_nodes"] {
        if let Some(children) = node[child_key].as_array() {
            for child in children {
                walk_sway(child, node_workspace, out);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sway_tree_walk_extracts_views() {
        let tree: serde_json::Value = serde_json::from_str(
            r#"{
                "type": "root",
                "nodes": [{
                    "type": "workspace", "name": "3",
                    "nodes": [
                        {"type": "con", "id": 7, "name": "vim", "app_id": "foot",
                         "focused": true, "nodes": []},
                        {"type": "con", "id": 9, "name": null, "nodes": []}
                    ],
                    "floating_nodes": [
                        {"type": "floating_con", "id": 11, "name": "calc",
                         "window_properties": {"class": "Galculator"},
                         "focused": false, "nodes": []}
                    ]
                }]
            }"#,
        )
        .unwrap();
        let mut windows = Vec::new();
        walk_sway(&tree, None, &mut windows);
        assert_eq!(windows.len(), 2);
        assert_eq!(windows[0].class, "foot");
        assert_eq!(windows[0].workspace, "3");
        assert!(windows[0].focused);
        assert_eq!(windows[1].class, "Galculator");
    }

    #[test]
    fn format_and_match_text() {
        let window = WindowInfo {
            id: "0x1".into(),
            title: "README.md - vim".into(),
            class: "foot".into(),
            workspace: "3".into(),
            focused: false,
            focus_order: 1,
            on_current_workspace: true,
        };
        let item = window_item(&window, &WindowConfig::default());
        assert_eq!(item.display, "3   foot   README.md - vim");
        assert_eq!(item.match_text, "README.md - vim foot");
    }
}
