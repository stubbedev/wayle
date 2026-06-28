//! Re-queries sway and rebuilds the reactive [`Property`](wayle_core::Property)
//! fields, preserving [`Arc`] identity for entities that still exist so that
//! per-field watchers only fire when that specific field changes.

use std::{collections::HashMap, sync::Arc};

use tracing::warn;

use super::MonitoringHandles;
use crate::{
    core::{Window, WindowSnapshot, Workspace},
    ipc::SwayCommandClient,
    types::TreeNode,
};

/// Re-queries `GET_WORKSPACES` and refreshes the workspaces field.
pub(super) async fn refresh_workspaces(client: &SwayCommandClient, handles: &MonitoringHandles) {
    let replies = match client.get_workspaces().await {
        Ok(replies) => replies,
        Err(err) => {
            warn!(error = %err, "sway get_workspaces failed");
            return;
        }
    };

    let current = handles.workspaces.get();
    let mut updated: HashMap<u64, Arc<Workspace>> = HashMap::with_capacity(replies.len());

    for reply in &replies {
        let id = reply.id as u64;
        match current.get(&id) {
            Some(existing) => {
                existing.refresh_from_reply(reply);
                updated.insert(id, Arc::clone(existing));
            }
            None => {
                updated.insert(id, Arc::new(Workspace::from_reply(reply)));
            }
        }
    }

    handles.workspaces.set(updated);
}

/// Re-queries `GET_TREE` and refreshes the windows field.
pub(super) async fn refresh_windows(client: &SwayCommandClient, handles: &MonitoringHandles) {
    let tree = match client.get_tree().await {
        Ok(tree) => tree,
        Err(err) => {
            warn!(error = %err, "sway get_tree failed");
            return;
        }
    };

    let mut snapshots = Vec::new();
    collect_windows(&tree, None, &mut snapshots);

    let current = handles.windows.get();
    let mut updated: HashMap<u64, Arc<Window>> = HashMap::with_capacity(snapshots.len());

    for snapshot in &snapshots {
        match current.get(&snapshot.id) {
            Some(existing) => {
                existing.refresh_from_snapshot(snapshot);
                updated.insert(snapshot.id, Arc::clone(existing));
            }
            None => {
                updated.insert(snapshot.id, Arc::new(Window::from_snapshot(snapshot)));
            }
        }
    }

    handles.windows.set(updated);
}

/// Re-queries `GET_INPUTS` and refreshes the keyboard-layout field from the
/// first keyboard reporting an active layout.
pub(super) async fn refresh_keyboard_layout(
    client: &SwayCommandClient,
    handles: &MonitoringHandles,
) {
    let inputs = match client.get_inputs().await {
        Ok(inputs) => inputs,
        Err(err) => {
            warn!(error = %err, "sway get_inputs failed");
            return;
        }
    };

    let layout = inputs
        .into_iter()
        .filter(|input| input.input_type == "keyboard")
        .find_map(|input| input.xkb_active_layout_name);

    handles.keyboard_layout.set(layout);
}

/// Walks the container tree, recording the enclosing workspace id for each leaf
/// window node.
fn collect_windows(node: &TreeNode, workspace_id: Option<u64>, out: &mut Vec<WindowSnapshot>) {
    let workspace_id = if node.node_type == "workspace" {
        Some(node.id as u64)
    } else {
        workspace_id
    };

    if node.is_window() {
        out.push(WindowSnapshot {
            id: node.id as u64,
            title: node.name.clone(),
            app_id: node.resolved_app_id(),
            pid: node.pid,
            workspace_id,
            is_focused: node.focused,
            is_floating: node.is_floating(),
            is_urgent: node.urgent,
        });
        return;
    }

    for child in node.nodes.iter().chain(node.floating_nodes.iter()) {
        collect_windows(child, workspace_id, out);
    }
}
