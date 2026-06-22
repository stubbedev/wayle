//! Data-transfer types deserialized from sway's i3 IPC replies.

use serde::Deserialize;

/// One entry of a `GET_WORKSPACES` reply.
///
/// sway numbers workspaces with `num` (`-1` when the workspace has no leading
/// number) and always assigns a unique `name` and a stable container `id`.
#[derive(Debug, Clone, Deserialize)]
pub(crate) struct WorkspaceReply {
    /// Stable sway container id for the workspace.
    pub id: i64,
    /// Leading workspace number, or `-1` for purely named workspaces.
    pub num: i32,
    /// Workspace name as shown by sway.
    pub name: String,
    /// Whether the workspace is currently visible on its output.
    pub visible: bool,
    /// Whether the workspace holds input focus.
    pub focused: bool,
    /// Whether any window on the workspace requested attention.
    pub urgent: bool,
    /// Connector name of the output the workspace is on.
    #[serde(default)]
    pub output: String,
}

/// X11 window properties, present only for XWayland surfaces.
#[derive(Debug, Clone, Default, Deserialize)]
pub(crate) struct WindowProperties {
    /// X11 `WM_CLASS` class, used as the app id fallback for XWayland windows.
    #[serde(default)]
    pub class: Option<String>,
}

/// A node in a `GET_TREE` reply.
///
/// sway models everything as a tree of containers; leaf containers (no child
/// `nodes` or `floating_nodes`) of type `con`/`floating_con` are application
/// windows.
#[derive(Debug, Clone, Deserialize)]
pub(crate) struct TreeNode {
    /// Stable sway container id.
    pub id: i64,
    /// Node kind: `root`, `output`, `workspace`, `con`, `floating_con`.
    #[serde(rename = "type", default)]
    pub node_type: String,
    /// Window title, or workspace/output name depending on `node_type`.
    #[serde(default)]
    pub name: Option<String>,
    /// Wayland application id.
    #[serde(default)]
    pub app_id: Option<String>,
    /// Process id of the client, when sway can determine it.
    #[serde(default)]
    pub pid: Option<i32>,
    /// Whether this node holds input focus.
    #[serde(default)]
    pub focused: bool,
    /// Whether this node has signalled urgency.
    #[serde(default)]
    pub urgent: bool,
    /// X11 window properties for XWayland surfaces.
    #[serde(default)]
    pub window_properties: Option<WindowProperties>,
    /// Tiled child nodes.
    #[serde(default)]
    pub nodes: Vec<TreeNode>,
    /// Floating child nodes.
    #[serde(default)]
    pub floating_nodes: Vec<TreeNode>,
}

impl TreeNode {
    /// Whether this node is a leaf application window rather than a container,
    /// output, or workspace.
    pub(crate) fn is_window(&self) -> bool {
        matches!(self.node_type.as_str(), "con" | "floating_con")
            && self.nodes.is_empty()
            && self.floating_nodes.is_empty()
    }

    /// Whether this node is currently floating.
    pub(crate) fn is_floating(&self) -> bool {
        self.node_type == "floating_con"
    }

    /// Resolved application id: the Wayland `app_id`, falling back to the
    /// XWayland `WM_CLASS` class.
    pub(crate) fn resolved_app_id(&self) -> Option<String> {
        self.app_id.clone().or_else(|| {
            self.window_properties
                .as_ref()
                .and_then(|properties| properties.class.clone())
        })
    }
}

/// Wraps a `RUN_COMMAND` reply entry to surface per-command failures.
#[derive(Debug, Clone, Deserialize)]
pub(crate) struct CommandResult {
    /// Whether sway accepted the command.
    pub success: bool,
    /// Human-readable rejection reason when `success` is `false`.
    #[serde(default)]
    pub error: Option<String>,
}

/// The `GET_VERSION` reply.
#[derive(Debug, Clone, Deserialize)]
pub(crate) struct VersionReply {
    /// Human-readable sway version string.
    pub human_readable: String,
}

/// One entry of a `GET_INPUTS` reply.
#[derive(Debug, Clone, Deserialize)]
pub(crate) struct InputReply {
    /// Device kind, e.g. `keyboard`, `pointer`, `touch`.
    #[serde(rename = "type", default)]
    pub input_type: String,
    /// Name of the active XKB layout, present only for keyboards.
    #[serde(default)]
    pub xkb_active_layout_name: Option<String>,
}
