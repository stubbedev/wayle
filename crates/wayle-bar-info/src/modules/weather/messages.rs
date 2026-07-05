use std::{rc::Rc, sync::Arc};

use wayle_config::ConfigService;
use wayle_weather::WeatherService;
use wayle_widgets::prelude::BarSettings;

use crate::shell::bar::dropdowns::DropdownRegistry;

pub struct WeatherInit {
    pub settings: BarSettings,
    pub weather: Arc<WeatherService>,
    pub config: Arc<ConfigService>,
    pub dropdowns: Rc<DropdownRegistry>,
}

#[derive(Debug)]
pub enum WeatherMsg {
    LeftClick,
    RightClick,
    MiddleClick,
    ScrollUp,
    ScrollDown,
}

#[derive(Debug)]
pub enum WeatherCmd {
    UpdateLabel(String),
    UpdateIcon(String),
}
