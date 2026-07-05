use std::sync::Arc;

use wayle_config::{ConfigProperty, ConfigService};

pub struct SeparatorInit {
    pub is_vertical: ConfigProperty<bool>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum SeparatorCmd {
    StylingChanged,
    OrientationChanged(bool),
}
