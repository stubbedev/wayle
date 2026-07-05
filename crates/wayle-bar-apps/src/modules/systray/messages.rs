use std::sync::Arc;

use wayle_config::{ConfigProperty, ConfigService};
use wayle_systray::{SystemTrayService, core::item::TrayItem};

pub struct SystrayInit {
    pub is_vertical: ConfigProperty<bool>,
    pub systray: Arc<SystemTrayService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum SystrayMsg {}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum SystrayCmd {
    ItemsChanged(Vec<Arc<TrayItem>>),
    StylingChanged,
    OrientationChanged(bool),
}
