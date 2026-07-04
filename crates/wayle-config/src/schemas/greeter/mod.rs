use schemars::schema_for;
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
    schemas::{lock::LockBackground, styling::HexColor},
};

fn black() -> HexColor {
    HexColor::new("#000000").unwrap_or_default()
}

/// Greeter (display manager): the pre-login screen `wayle-greeter` renders as
/// a greetd greeter.
///
/// The greeter reads the system config (`/etc/wayle/config.toml`), so these
/// settings take effect there — copy or symlink your user config if you want
/// the login screen to follow it.
#[wayle_config(i18n_prefix = "settings-greeter")]
pub struct GreeterConfig {
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

    /// Show a clock above the login form.
    #[serde(rename = "show-clock")]
    #[default(true)]
    pub show_clock: ConfigProperty<bool>,

    /// `strftime` format for the greeter time.
    #[serde(rename = "clock-format")]
    #[default(String::from("%H:%M"))]
    pub clock_format: ConfigProperty<String>,

    /// `strftime` format for the greeter date.
    #[serde(rename = "date-format")]
    #[default(String::from("%A, %B %-d"))]
    pub date_format: ConfigProperty<String>,

    /// Show clickable avatars for the machine's login users.
    #[serde(rename = "show-user-list")]
    #[default(true)]
    pub show_user_list: ConfigProperty<bool>,

    /// Show the shutdown/reboot buttons at the bottom of the screen.
    #[serde(rename = "show-power-buttons")]
    #[default(true)]
    pub show_power_buttons: ConfigProperty<bool>,

    /// Xcursor theme used on the login screen (empty = system default).
    #[serde(rename = "cursor-theme")]
    #[default(String::new())]
    pub cursor_theme: ConfigProperty<String>,

    /// Logical cursor size on the login screen. Scaled automatically per
    /// display, so HiDPI outputs get a matching high-resolution cursor.
    #[serde(rename = "cursor-size")]
    #[default(24u32)]
    pub cursor_size: ConfigProperty<u32>,
}

impl ModuleInfoProvider for GreeterConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("greeter"),
            schema: || schema_for!(GreeterConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

crate::register_module!(GreeterConfig);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_match_previous_lock_derived_behaviour() {
        let greeter = GreeterConfig::default();
        assert_eq!(greeter.background_mode.get(), LockBackground::Color);
        assert_eq!(greeter.background_color.get().as_str(), "#000000");
        assert!(greeter.show_clock.get());
        assert_eq!(greeter.clock_format.get(), "%H:%M");
        assert!(greeter.show_user_list.get());
        assert!(greeter.show_power_buttons.get());
        assert_eq!(greeter.cursor_size.get(), 24);
        assert!(greeter.cursor_theme.get().is_empty());
    }

    #[test]
    fn config_includes_greeter_section() {
        let config = crate::Config::default();
        assert!(config.greeter.show_clock.get());
    }
}
