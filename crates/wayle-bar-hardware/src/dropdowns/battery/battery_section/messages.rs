use std::sync::Arc;

use wayle_battery::BatteryService;

pub struct BatterySectionInit {
    pub battery: Arc<BatteryService>,
}

#[derive(Debug)]
pub enum BatterySectionInput {
    ChargeLimitToggled(bool),
}

#[derive(Debug)]
pub enum BatterySectionCmd {
    BatteryStateChanged,
}
