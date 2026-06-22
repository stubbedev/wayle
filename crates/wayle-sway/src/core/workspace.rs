//! Reactive wrapper for a sway workspace.

use wayle_core::Property;

use crate::types::WorkspaceReply;

/// A sway workspace with reactive state.
///
/// Instances from [`SwayService`](crate::SwayService) fields update in place as
/// sway emits events, so watching any field reflects live state.
#[derive(Debug, Clone)]
pub struct Workspace {
    /// Stable sway container id for the workspace.
    pub id: Property<u64>,
    /// Leading workspace number, or `-1` for purely named workspaces.
    pub num: Property<i32>,
    /// Workspace name as shown by sway.
    pub name: Property<String>,
    /// Connector name of the output the workspace is on.
    pub output: Property<String>,
    /// Whether any window on this workspace requested attention.
    pub is_urgent: Property<bool>,
    /// Whether this workspace is currently visible on its output.
    pub is_active: Property<bool>,
    /// Whether this workspace holds input focus.
    pub is_focused: Property<bool>,
}

impl Workspace {
    pub(crate) fn from_reply(reply: &WorkspaceReply) -> Self {
        Self {
            id: Property::new(reply.id as u64),
            num: Property::new(reply.num),
            name: Property::new(reply.name.clone()),
            output: Property::new(reply.output.clone()),
            is_urgent: Property::new(reply.urgent),
            is_active: Property::new(reply.visible),
            is_focused: Property::new(reply.focused),
        }
    }

    pub(crate) fn refresh_from_reply(&self, reply: &WorkspaceReply) {
        self.id.set(reply.id as u64);
        self.num.set(reply.num);
        self.name.set(reply.name.clone());
        self.output.set(reply.output.clone());
        self.is_urgent.set(reply.urgent);
        self.is_active.set(reply.visible);
        self.is_focused.set(reply.focused);
    }
}

/// Keyed on `id`. Two workspaces are equal iff they share an id, regardless of
/// field content, so collection-level `PartialEq` compares set-membership.
impl PartialEq for Workspace {
    fn eq(&self, other: &Self) -> bool {
        self.id.get() == other.id.get()
    }
}
