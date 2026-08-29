use std::sync::Arc;

use tracing::instrument;
use zbus::{fdo, interface};

use crate::{service::PowerProfilesService, types::profile::PowerProfile};

#[derive(Debug)]
pub(crate) struct PowerProfilesDaemon {
    pub service: Arc<PowerProfilesService>,
}

#[interface(name = "com.wayle.PowerProfiles1")]
impl PowerProfilesDaemon {
    #[instrument(skip(self), fields(profile = %profile))]
    pub async fn set_profile(&self, profile: String) -> fdo::Result<()> {
        let power_profile = match profile.as_str() {
            "power-saver" => PowerProfile::PowerSaver,
            "balanced" => PowerProfile::Balanced,
            "performance" => PowerProfile::Performance,
            _ => {
                return Err(fdo::Error::InvalidArgs(format!(
                    "Invalid profile: {profile}. Expected: power-saver, balanced, performance"
                )));
            }
        };

        self.service
            .power_profiles
            .set_active_profile(power_profile)
            .await
            .map_err(|e| fdo::Error::Failed(e.to_string()))
    }

    #[instrument(skip(self))]
    pub async fn cycle(&self) -> fdo::Result<()> {
        let current = self.service.power_profiles.active_profile.get();
        let next = match current {
            PowerProfile::PowerSaver | PowerProfile::Unknown => PowerProfile::Balanced,
            PowerProfile::Balanced => PowerProfile::Performance,
            PowerProfile::Performance => PowerProfile::PowerSaver,
        };

        self.service
            .power_profiles
            .set_active_profile(next)
            .await
            .map_err(|e| fdo::Error::Failed(e.to_string()))
    }

    #[instrument(skip(self))]
    pub async fn list_profiles(&self) -> Vec<String> {
        self.service
            .power_profiles
            .profiles
            .get()
            .iter()
            .map(|p| p.profile.to_string())
            .collect()
    }

    #[zbus(property)]
    pub fn active_profile(&self) -> String {
        self.service.power_profiles.active_profile.get().to_string()
    }

    #[zbus(property)]
    pub fn performance_degraded(&self) -> String {
        self.service
            .power_profiles
            .performance_degraded
            .get()
            .to_string()
    }

    #[zbus(property)]
    pub fn profile_count(&self) -> u32 {
        u32::try_from(self.service.power_profiles.profiles.get().len()).unwrap_or(u32::MAX)
    }
}
