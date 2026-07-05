use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_weather::{WeatherErrorKind, WeatherService};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WeatherPage {
    Loading,
    Loaded,
    Error,
}

impl WeatherPage {
    pub fn name(self) -> &'static str {
        match self {
            Self::Loading => "loading",
            Self::Loaded => "loaded",
            Self::Error => "error",
        }
    }
}

pub struct WeatherDropdownInit {
    pub weather: Arc<WeatherService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum WeatherDropdownInput {
    Retry,
}

#[derive(Debug)]
pub enum WeatherDropdownCmd {
    ScaleChanged(f32),
    PageChanged {
        page: WeatherPage,
        error: Option<WeatherErrorKind>,
    },
}
