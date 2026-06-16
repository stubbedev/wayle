//! Shell-specific services that run alongside the UI.

pub mod idle_inhibit;
pub mod recorder;
pub mod shell_ipc;
pub mod widget_ipc;

pub use idle_inhibit::IdleInhibitService;
pub use recorder::RecorderService;
pub use shell_ipc::ShellIpcService;
pub use widget_ipc::{ToastBus, WidgetBus};
