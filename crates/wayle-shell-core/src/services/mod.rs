//! Shell-specific services shared with the bar module crates.
//!
//! Window-bound services (file chooser, lock, print, …) stay in wayle-shell;
//! only the services the bar modules consume live here.

pub mod idle_inhibit;
pub mod mail;
pub mod recorder;
pub mod shell_ipc;
pub mod widget_ipc;

pub use idle_inhibit::IdleInhibitService;
pub use mail::MailService;
pub use recorder::RecorderService;
pub use shell_ipc::ShellIpcService;
pub use widget_ipc::{ToastBus, ToastRequest, WidgetBus};
