mod types;

use schemars::schema_for;
pub use types::AnimationType;
use wayle_derive::wayle_config;

use crate::{
    ConfigProperty,
    docs::{ConfigGroup, GroupDefaults, ModuleInfo, ModuleInfoProvider},
};

/// Enter/exit and change animations for transient surfaces.
#[wayle_config(i18n_prefix = "settings-animations")]
pub struct AnimationsConfig {
    /// Enable enter/exit animations (OSD, toasts, notifications) and
    /// icon-change crossfades. When disabled, surfaces appear instantly.
    #[default(true)]
    pub enabled: ConfigProperty<bool>,

    /// Animation duration in milliseconds.
    #[default(200u32)]
    pub duration: ConfigProperty<u32>,

    /// Transition style used for enter/exit of the OSD, toasts, and
    /// notification cards.
    #[default(AnimationType::default())]
    pub transition: ConfigProperty<AnimationType>,
}

impl ModuleInfoProvider for AnimationsConfig {
    fn module_info() -> ModuleInfo {
        ModuleInfo {
            name: String::from("animations"),
            schema: || schema_for!(AnimationsConfig),
            layout_id: None,
            array_entry: false,
        }
    }

    fn groups() -> Vec<ConfigGroup> {
        GroupDefaults::standard()
    }
}

crate::register_module!(AnimationsConfig);
