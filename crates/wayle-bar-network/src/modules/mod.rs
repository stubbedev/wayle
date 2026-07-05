//! Bar module factories in this domain crate.

pub mod bluetooth;
pub mod mail;
pub mod netstat;
pub mod network;

#[allow(unused_imports)]
pub(crate) use wayle_shell_core::bar::{
    compositor,
    module_registry::{self as registry, ModuleFactory, ModuleInstance},
};
