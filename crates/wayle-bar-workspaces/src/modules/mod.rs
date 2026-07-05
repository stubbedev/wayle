//! Bar module factories in this domain crate.

pub mod hyprland_workspaces;
pub mod hyprsunset;
pub mod keybind_mode;
pub mod keyboard_input;
pub mod mango_workspaces;
pub mod niri_workspaces;
pub mod sway_workspaces;
pub mod window_title;

#[allow(unused_imports)]
pub(crate) use wayle_shell_core::bar::{
    compositor,
    module_registry::{self as registry, ModuleFactory, ModuleInstance},
};
