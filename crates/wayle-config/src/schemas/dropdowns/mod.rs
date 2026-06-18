mod types;

use schemars::schema_for;
pub use types::DropdownSize;
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
};

/// Per-dropdown foldout panel sizing.
///
/// Each field overrides the size of one bar widget dropdown. Unset fields keep
/// the built-in default (scaled by the global scale).
#[wayle_config(i18n_prefix = "settings-dropdowns")]
pub struct DropdownsConfig {
    /// Audio dropdown panel size.
    #[default(DropdownSize::default())]
    pub audio: ConfigProperty<DropdownSize>,

    /// Battery dropdown panel size.
    #[default(DropdownSize::default())]
    pub battery: ConfigProperty<DropdownSize>,

    /// Bluetooth dropdown panel size.
    #[default(DropdownSize::default())]
    pub bluetooth: ConfigProperty<DropdownSize>,

    /// Brightness dropdown panel size. Height grows to fit content.
    #[default(DropdownSize::default())]
    pub brightness: ConfigProperty<DropdownSize>,

    /// Calendar dropdown panel size. Height grows to fit content.
    #[default(DropdownSize::default())]
    pub calendar: ConfigProperty<DropdownSize>,

    /// Dashboard dropdown panel size. Height grows to fit content.
    #[default(DropdownSize::default())]
    pub dashboard: ConfigProperty<DropdownSize>,

    /// Mail dropdown panel size. Height grows to fit content.
    #[default(DropdownSize::default())]
    pub mail: ConfigProperty<DropdownSize>,

    /// Media dropdown panel size.
    #[default(DropdownSize::default())]
    pub media: ConfigProperty<DropdownSize>,

    /// Network dropdown panel size.
    #[default(DropdownSize::default())]
    pub network: ConfigProperty<DropdownSize>,

    /// Notification dropdown panel size.
    #[default(DropdownSize::default())]
    pub notification: ConfigProperty<DropdownSize>,

    /// Weather dropdown panel size.
    #[default(DropdownSize::default())]
    pub weather: ConfigProperty<DropdownSize>,
}

impl ModuleInfoProvider for DropdownsConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("dropdowns"),
            schema: || schema_for!(DropdownsConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

crate::register_module!(DropdownsConfig);
