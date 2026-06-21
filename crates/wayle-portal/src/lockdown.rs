//! `org.freedesktop.impl.portal.Lockdown`.
//!
//! A set of read-only boolean policy properties the frontend consults before
//! allowing certain actions. Wayle locks nothing down by default; every
//! property reports `false`. The values are wired as constants now so a future
//! `[lockdown]` config section can replace them without touching call sites.

use zbus::interface;

/// Lockdown policy interface.
pub struct Lockdown;

#[interface(name = "org.freedesktop.impl.portal.Lockdown")]
impl Lockdown {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        1
    }

    /// Whether printing is disabled.
    #[zbus(property, name = "disable-printing")]
    fn disable_printing(&self) -> bool {
        false
    }

    /// Whether saving files to disk is disabled.
    #[zbus(property, name = "disable-save-to-disk")]
    fn disable_save_to_disk(&self) -> bool {
        false
    }

    /// Whether launching applications to handle files/URIs is disabled.
    #[zbus(property, name = "disable-application-handlers")]
    fn disable_application_handlers(&self) -> bool {
        false
    }

    /// Whether location access is disabled.
    #[zbus(property, name = "disable-location")]
    fn disable_location(&self) -> bool {
        false
    }

    /// Whether camera access is disabled.
    #[zbus(property, name = "disable-camera")]
    fn disable_camera(&self) -> bool {
        false
    }

    /// Whether microphone access is disabled.
    #[zbus(property, name = "disable-microphone")]
    fn disable_microphone(&self) -> bool {
        false
    }

    /// Whether sound output is disabled.
    #[zbus(property, name = "disable-sound-output")]
    fn disable_sound_output(&self) -> bool {
        false
    }
}
