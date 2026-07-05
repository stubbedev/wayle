use std::sync::Arc;

use chrono::Weekday;
use wayle_config::ConfigService;

pub struct CalendarDropdownInit {
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum CalendarDropdownCmd {
    ScaleChanged(f32),
    TimeTick,
    FormatChanged(bool),
    ShowSecondsChanged(bool),
    WeekStartChanged(Weekday),
}
