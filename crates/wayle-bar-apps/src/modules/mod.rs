//! Bar module factories in this domain crate.

pub mod notification;
pub mod systray;
pub mod treeman;

#[allow(unused_imports)]
pub(crate) use wayle_shell_core::bar::{
    compositor,
    module_registry::{self as registry, ModuleFactory, ModuleInstance},
};
