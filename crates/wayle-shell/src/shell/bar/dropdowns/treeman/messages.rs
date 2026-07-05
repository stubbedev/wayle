use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_treeman::{TreemanService, TreemanStatus};

use crate::services::ToastBus;

pub(crate) struct TreemanDropdownInit {
    pub treeman: Arc<TreemanService>,
    pub config: Arc<ConfigService>,
    pub toast_bus: ToastBus,
}

#[derive(Debug)]
pub(crate) enum TreemanDropdownCmd {
    ScaleChanged(f32),
    StatusChanged(Option<TreemanStatus>),
}
