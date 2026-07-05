//! Bar module factories in this domain crate.

pub mod battery;
pub mod brightness;
pub mod cpu;
pub mod idle_inhibit;
pub mod power;
pub mod power_profiles;
pub mod ram;
pub mod screenshot;
pub mod storage;

#[allow(unused_imports)]
pub(crate) use wayle_shell_core::bar::{
    compositor,
    module_registry::{self as registry, ModuleFactory, ModuleInstance},
};
