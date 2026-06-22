//! Message types for the [`SwayWorkspaces`] Relm4 component.
//!
//! [`SwayWorkspaces`]: super::SwayWorkspaces

use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_sway::SwayService;
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub(crate) struct SwayWorkspacesInit {
    pub settings: BarSettings,
    pub sway: Arc<SwayService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub(crate) enum SwayWorkspacesMsg {
    LeftClick(u64),
    MiddleClick(u64),
    RightClick(u64),
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub(crate) enum SwayWorkspacesCmd {
    WorkspacesChanged,
    ConfigChanged,
    BlinkTick,
}
