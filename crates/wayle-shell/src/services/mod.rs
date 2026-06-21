//! Shell-specific services that run alongside the UI.

pub mod file_chooser;
pub mod idle_inhibit;
pub mod mail;
pub mod power_menu;
pub mod recorder;
pub mod region_overlay;
pub mod screenshot;
pub mod share_picker;
pub mod shell_ipc;
pub mod widget_ipc;

pub use idle_inhibit::IdleInhibitService;
pub use mail::MailService;
pub use recorder::RecorderService;
pub use shell_ipc::ShellIpcService;
pub use widget_ipc::{ToastBus, WidgetBus};
