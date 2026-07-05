use super::{SunTimes, helpers};

impl SunTimes {
    pub fn refresh(&mut self) {
        let Some(weather) = self.weather.weather.get() else {
            return;
        };

        let format = self.config.config().modules.weather.time_format.get();
        self.sunrise = helpers::format_time(weather.astronomy.sunrise, format);
        self.sunset = helpers::format_time(weather.astronomy.sunset, format);
    }

    pub fn sunrise(&self) -> &str {
        &self.sunrise
    }

    pub fn sunset(&self) -> &str {
        &self.sunset
    }
}
