mod types;

#[cfg(feature = "schema")]
use schemars::schema_for;
pub use types::LockBackground;
use wayle_derive::wayle_config;

#[cfg(feature = "schema")]
use crate::docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider};
use crate::{ConfigProperty, schemas::styling::HexColor};

fn black() -> HexColor {
    HexColor::new("#000000").unwrap_or_default()
}

/// Lock screen: a secure session lock rendered by Wayle via `ext-session-lock-v1`.
///
/// When `enabled`, Wayle locks the session in response to the logind `Lock`
/// signal (`loginctl lock-session`), the `wayle lock` CLI, or the shell IPC
/// `lock()` method. The lock surface grabs all input and survives client
/// crashes (the compositor shows a solid color), unlike a layer-shell overlay.
#[wayle_config(i18n_prefix = "settings-lock")]
pub struct LockConfig {
    /// Let Wayle handle session locking. When off, lock requests are ignored
    /// and an external locker (e.g. hyprlock) stays responsible.
    #[default(true)]
    pub enabled: ConfigProperty<bool>,

    /// How the background is drawn: solid color, an image, or the wallpaper.
    #[serde(rename = "background-mode")]
    #[default(LockBackground::default())]
    pub background_mode: ConfigProperty<LockBackground>,

    /// Background image path (used when `background-mode = "image"`).
    #[serde(rename = "background-image")]
    #[default(String::new())]
    pub background_image: ConfigProperty<String>,

    /// Background fill color (used when `background-mode = "color"`).
    #[serde(rename = "background-color")]
    #[default(black())]
    pub background_color: ConfigProperty<HexColor>,

    /// Gaussian blur radius applied to image/wallpaper backgrounds (0 = none).
    #[default(0u32)]
    pub blur: ConfigProperty<u32>,

    /// Show a clock on the lock screen.
    #[serde(rename = "show-clock")]
    #[default(true)]
    pub show_clock: ConfigProperty<bool>,

    /// `strftime` format for the lock-screen time.
    #[serde(rename = "clock-format")]
    #[default(String::from("%H:%M"))]
    pub clock_format: ConfigProperty<String>,

    /// `strftime` format for the lock-screen date.
    #[serde(rename = "date-format")]
    #[default(String::from("%A, %B %-d"))]
    pub date_format: ConfigProperty<String>,

    /// Grace window after locking during which the screen unlocks without a
    /// password (milliseconds, `0` = always require the password).
    #[serde(rename = "grace-period-ms")]
    #[default(0u32)]
    pub grace_period_ms: ConfigProperty<u32>,

    /// Maximum failed password attempts before further input is blocked
    /// (`0` = unlimited). The screen stays locked regardless.
    #[serde(rename = "max-attempts")]
    #[default(0u32)]
    pub max_attempts: ConfigProperty<u32>,

    /// Show the failed-attempt count on the lock screen.
    #[serde(rename = "show-failed-attempts")]
    #[default(true)]
    pub show_failed_attempts: ConfigProperty<bool>,

    /// Black out the lock screen after this idle time (milliseconds, `0` =
    /// never). This is a visual blank that hides the clock/prompt and dismisses
    /// on any key; true display power-off (DPMS) is left to your idle daemon.
    #[serde(rename = "blank-timeout-ms")]
    #[default(0u32)]
    pub blank_timeout_ms: ConfigProperty<u32>,

    /// PAM service name used to authenticate the unlock. Distro-dependent:
    /// `system-auth` (Arch/Fedora), `login`, or a custom `/etc/pam.d` entry.
    #[serde(rename = "pam-service")]
    #[default(String::from("system-auth"))]
    pub pam_service: ConfigProperty<String>,
}

#[cfg(feature = "schema")]
impl ModuleInfoProvider for LockConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("lock"),
            schema: || schema_for!(LockConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

#[cfg(feature = "schema")]
crate::register_module!(LockConfig);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_are_secure_and_sensible() {
        let lock = LockConfig::default();
        // Wayle handles locking by default.
        assert!(lock.enabled.get());
        // Solid black background unless configured otherwise.
        assert_eq!(lock.background_mode.get(), LockBackground::Color);
        assert_eq!(lock.background_color.get().as_str(), "#000000");
        // Secure defaults: no password-free grace, no attempt cap, never blank.
        assert_eq!(lock.grace_period_ms.get(), 0);
        assert_eq!(lock.max_attempts.get(), 0);
        assert_eq!(lock.blank_timeout_ms.get(), 0);
        // A distro-typical PAM service.
        assert_eq!(lock.pam_service.get(), "system-auth");
        // Clock shown with sane strftime formats.
        assert!(lock.show_clock.get());
        assert_eq!(lock.clock_format.get(), "%H:%M");
    }

    #[test]
    fn config_includes_lock_section() {
        let config = crate::Config::default();
        assert!(config.lock.enabled.get());
    }
}
