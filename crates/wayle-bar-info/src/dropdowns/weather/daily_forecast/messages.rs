use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_weather::WeatherService;

pub struct DailyForecastInit {
    pub weather: Arc<WeatherService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum DailyForecastCmd {
    WeatherChanged,
}
