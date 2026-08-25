use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_treeman::{TreemanService, TreemanStatus};

use crate::services::ToastBus;

pub struct TreemanDropdownInit {
    pub treeman: Arc<TreemanService>,
    pub config: Arc<ConfigService>,
    pub toast_bus: ToastBus,
}

#[derive(Debug)]
pub enum TreemanDropdownMsg {
    /// Drill into one worktree's detail page, keyed by its absolute path.
    OpenDetail(String),
    /// Return from the detail page to the repo list.
    Back,
}

#[derive(Debug)]
pub enum TreemanDropdownCmd {
    ScaleChanged(f32),
    StatusChanged(Option<TreemanStatus>),
}
