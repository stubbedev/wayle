use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_weather::WeatherService;

pub struct StatsGridInit {
    pub weather: Arc<WeatherService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum StatsGridCmd {
    WeatherChanged,
}
