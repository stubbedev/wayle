use std::sync::Arc;

use wayle_core::Property;
use wayle_power_profiles::{PowerProfilesService, types::profile::PowerProfile};

pub struct PowerProfileInit {
    pub power_profiles: Property<Option<Arc<PowerProfilesService>>>,
}

#[derive(Debug)]
pub enum PowerProfileInput {
    ProfileSelected(PowerProfile),
}

#[derive(Debug)]
pub enum PowerProfileCmd {
    ProfileChanged(PowerProfile),
    AvailableProfilesChanged(Vec<PowerProfile>),
    ServiceAvailable(Arc<PowerProfilesService>),
    ServiceUnavailable,
}
