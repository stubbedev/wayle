//! Reactive bindings to the sway compositor via its i3 IPC socket.
//!
//! [`SwayService::new`] connects to `$SWAYSOCK`, subscribes to sway's
//! `workspace` and `window` events, and exposes compositor state through
//! [`Property<T>`] fields that stay in sync automatically.
//!
//! ```no_run
//! use wayle_sway::SwayService;
//! use futures::StreamExt;
//!
//! # async fn example() -> wayle_sway::Result<()> {
//! let service = SwayService::new().await?;
//!
//! for workspace in service.workspaces.get().values() {
//!     println!("{} on {}", workspace.num.get(), workspace.output.get());
//! }
//!
//! let mut workspaces = service.workspaces.watch();
//! while let Some(snapshot) = workspaces.next().await {
//!     println!("{} workspaces", snapshot.len());
//! }
//! # Ok(())
//! # }
//! ```
//!
//! # Reactive properties
//!
//! Every [`Property<T>`] supports `.get()` (cloned snapshot) and `.watch()`
//! (stream that yields the current value, then each subsequent change).
//!
//! - [`SwayService::workspaces`] - all workspaces keyed by stable id.
//! - [`SwayService::windows`] - every leaf window keyed by stable id.
//! - [`SwayService::keyboard_layout`] - the active XKB layout name.
//!
//! Fields on each [`Workspace`](core::Workspace) and [`Window`](core::Window)
//! are themselves [`Property<T>`] values, and the service preserves their
//! [`Arc`](std::sync::Arc) identity across refreshes, so watching one field
//! only fires when that specific field changes.
//!
//! # Commands
//!
//! sway speaks the i3 IPC protocol. [`SwayService::run_command`] runs raw sway
//! commands; the wrappers [`SwayService::focus_workspace`],
//! [`SwayService::focus_next_on_output`], [`SwayService::focus_prev_on_output`],
//! and [`SwayService::focus_back_and_forth`] cover the common cases.
//!
//! [`Property<T>`]: wayle_core::Property

mod constants;
mod error;
mod ipc;
mod monitoring;
mod service;
mod types;

pub mod core;

pub use error::{Error, Result, SocketKind};
pub use service::{SwayService, WorkspaceRef};

#[doc = include_str!("../README.md")]
#[cfg(doctest)]
pub struct ReadmeDocTests;
