use std::{collections::HashMap, rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_hyprland::{Address, HyprlandService, WorkspaceId};
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub struct WorkspacesInit {
    pub settings: BarSettings,
    pub hyprland: Option<Arc<HyprlandService>>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum WorkspacesMsg {
    WorkspaceClicked(WorkspaceId),
    MiddleClick(WorkspaceId),
    RightClick(WorkspaceId),
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub enum WorkspacesCmd {
    WorkspacesChanged,
    ClientsChanged,
    ActiveWorkspaceChanged(WorkspaceId),
    MonitorFocused {
        monitor: String,
        workspace_id: WorkspaceId,
    },
    TitleChanged,
    ConfigChanged,
    HyprlandConfigReloaded,
    UrgentWindow(Address),
    WindowFocused(Address),
    BlinkTick,
    WorkspaceRulesLoaded(HashMap<WorkspaceId, String>),
}
