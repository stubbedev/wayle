//! Reactive wrapper for a sway window (a leaf container).

use wayle_core::Property;

/// Plain-data view of one window, assembled while walking the container tree.
#[derive(Debug, Clone)]
pub(crate) struct WindowSnapshot {
    pub id: u64,
    pub title: Option<String>,
    pub app_id: Option<String>,
    pub pid: Option<i32>,
    pub workspace_id: Option<u64>,
    pub is_focused: bool,
    pub is_floating: bool,
    pub is_urgent: bool,
}

/// A sway toplevel window with reactive state.
///
/// Instances from [`SwayService`](crate::SwayService) fields update in place as
/// sway emits events.
#[derive(Debug, Clone)]
pub struct Window {
    /// Stable sway container id.
    pub id: Property<u64>,
    /// Window title if set by the application.
    pub title: Property<Option<String>>,
    /// Wayland `app_id`, or the XWayland `WM_CLASS` class as a fallback.
    pub app_id: Property<Option<String>>,
    /// PID of the client process, when sway can determine it.
    pub pid: Property<Option<i32>>,
    /// Id of the workspace this window is on.
    pub workspace_id: Property<Option<u64>>,
    /// Whether this window has input focus.
    pub is_focused: Property<bool>,
    /// Whether this window is floating.
    pub is_floating: Property<bool>,
    /// Whether the window has signalled urgency.
    pub is_urgent: Property<bool>,
}

impl Window {
    pub(crate) fn from_snapshot(snapshot: &WindowSnapshot) -> Self {
        Self {
            id: Property::new(snapshot.id),
            title: Property::new(snapshot.title.clone()),
            app_id: Property::new(snapshot.app_id.clone()),
            pid: Property::new(snapshot.pid),
            workspace_id: Property::new(snapshot.workspace_id),
            is_focused: Property::new(snapshot.is_focused),
            is_floating: Property::new(snapshot.is_floating),
            is_urgent: Property::new(snapshot.is_urgent),
        }
    }

    pub(crate) fn refresh_from_snapshot(&self, snapshot: &WindowSnapshot) {
        self.id.set(snapshot.id);
        self.title.set(snapshot.title.clone());
        self.app_id.set(snapshot.app_id.clone());
        self.pid.set(snapshot.pid);
        self.workspace_id.set(snapshot.workspace_id);
        self.is_focused.set(snapshot.is_focused);
        self.is_floating.set(snapshot.is_floating);
        self.is_urgent.set(snapshot.is_urgent);
    }
}

/// Keyed on `id`. Two windows are equal iff they share an id, regardless of
/// field content, so collection-level `PartialEq` compares set-membership.
impl PartialEq for Window {
    fn eq(&self, other: &Self) -> bool {
        self.id.get() == other.id.get()
    }
}
