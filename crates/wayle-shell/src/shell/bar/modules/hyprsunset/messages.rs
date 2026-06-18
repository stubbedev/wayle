use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_widgets::prelude::BarSettings;

use super::helpers::HyprsunsetState;
use crate::shell::bar::dropdowns::DropdownRegistry;

pub(crate) struct HyprsunsetInit {
    pub settings: BarSettings,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub(crate) enum HyprsunsetMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub(crate) enum HyprsunsetCmd {
    ConfigChanged,
    StateChanged(Option<HyprsunsetState>),
    /// Re-evaluate the sunrise/sunset auto-schedule.
    TickSchedule,
    /// GeoClue resolved a location for the auto-schedule (latitude, longitude).
    LocationResolved(f64, f64),
}
