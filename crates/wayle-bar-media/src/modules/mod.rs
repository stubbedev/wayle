//! Bar module factories in this domain crate.

pub mod cava;
pub mod media;
pub mod microphone;
pub mod recorder;
pub mod volume;

#[allow(unused_imports)]
pub(crate) use wayle_shell_core::bar::{
    compositor,
    module_registry::{self as registry, ModuleFactory, ModuleInstance},
};
