//! Shared infrastructure for the Wayle shell and its bar module crates.
//!
//! Split out of `wayle-shell` so the bar module crates can compile in
//! parallel: this crate holds everything they share — the [`ShellServices`]
//! container, i18n, small process/template/glob utilities, and the bar
//! module/dropdown registry plumbing — while `wayle-shell` keeps the windows
//! and the layout aggregation.
// Most items here were `pub(crate)` inside wayle-shell before the split; the
// crate boundary forces them `pub`, but they are internal API for the shell
// crates only, not a documented public surface.
#![allow(missing_docs)]
#![allow(clippy::missing_errors_doc, clippy::missing_panics_doc)]

pub mod bar;
pub mod glob;
pub mod helpers;
pub mod i18n;
pub mod notification_icons;
pub mod notify;
pub mod process;
pub mod services;
pub mod shell_services;
pub mod template;

pub use shell_services::ShellServices;
