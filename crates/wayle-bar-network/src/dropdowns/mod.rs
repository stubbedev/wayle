//! Dropdown factories in this domain crate.

pub mod bluetooth;
pub mod mail;
pub mod network;

#[allow(unused_imports)]
pub(crate) use wayle_shell_core::bar::{
    dropdown_registry::{
        self as registry, DropdownFactory, DropdownInstance, DropdownRegistry, dispatch_click,
        dispatch_click_widget, require_service,
    },
    resolve_content_height, resolve_dimension,
};
