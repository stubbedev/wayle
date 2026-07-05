//! Reactive treeman worktree-health status service.
//!
//! [`TreemanService`] mirrors `treeman status --format json` into a reactive
//! [`Property`](wayle_core::Property), kept live by subscribing to the treeman
//! daemon's event stream and refetching on each (debounced) change. When the
//! daemon is not running it degrades to a slow poll of the same command.
//!
//! ```no_run
//! use wayle_treeman::TreemanService;
//!
//! let treeman = TreemanService::builder().build();
//! if let Some(status) = treeman.status.get() {
//!     println!("{} worktrees, {} failed", status.total, status.failed);
//! }
//! ```

/// Error and result types.
pub mod error;

/// Data model mirroring `treeman status --format json`.
pub mod model;

mod service;
mod socket;

pub use error::{Error, Result};
pub use model::{Bucket, TreemanRepo, TreemanStatus, TreemanWorktree};
pub use service::{Action, TreemanService, TreemanServiceBuilder};
