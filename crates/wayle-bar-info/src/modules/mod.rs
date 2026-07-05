//! Bar module factories in this domain crate.

pub mod clock;
pub mod custom;
pub mod dashboard;
pub mod separator;
pub mod weather;
pub mod world_clock;

#[allow(unused_imports)]
pub(crate) use wayle_shell_core::bar::{
    compositor,
    module_registry::{self as registry, ModuleFactory, ModuleInstance},
};
