//! Shell-specific services that run alongside the UI.

pub mod color_picker;
pub mod file_chooser;
pub mod launcher;
pub mod lock;
pub mod portal_dialogs;
pub mod power_menu;
pub mod print;
pub mod region_overlay;
pub mod screenshot;
pub mod share_picker;

// Bar-facing services moved to wayle-shell-core in the bar-crate split;
// re-exported so the old `crate::services::…` paths keep working.
pub use wayle_shell_core::services::{
    IdleInhibitService, MailService, RecorderService, ShellIpcService, ToastBus, WidgetBus,
    shell_ipc, widget_ipc,
};
