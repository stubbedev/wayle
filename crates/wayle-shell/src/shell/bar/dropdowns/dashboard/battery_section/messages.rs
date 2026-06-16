use std::sync::Arc;

use wayle_battery::{BatteryService, types::DeviceState};
use wayle_core::Property;
use wayle_power_profiles::{PowerProfilesService, types::profile::PowerProfile};

pub(crate) struct BatterySectionInit {
    pub battery: Option<Arc<BatteryService>>,
    pub power_profiles: Property<Option<Arc<PowerProfilesService>>>,
    /// Battery percent at or below which the warning state shows.
    pub warning: f64,
    /// Battery percent at or below which the critical state shows.
    pub critical: f64,
}

#[derive(Debug)]
pub(crate) enum BatterySectionCmd {
    StateChanged {
        percentage: f64,
        state: DeviceState,
        time_remaining_secs: i64,
    },
    PowerProfileChanged(PowerProfile),
    PowerProfilesAvailable(Arc<PowerProfilesService>),
    PowerProfilesUnavailable,
}
