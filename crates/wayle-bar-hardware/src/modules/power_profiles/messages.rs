use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_core::DeferredService;
use wayle_power_profiles::PowerProfilesService;
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub struct PowerProfilesInit {
    pub settings: BarSettings,
    pub power_profiles: DeferredService<PowerProfilesService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum PowerProfilesMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum PowerProfilesCmd {
    ServiceReady(Arc<PowerProfilesService>),
    StateChanged,
    ConfigChanged,
}
