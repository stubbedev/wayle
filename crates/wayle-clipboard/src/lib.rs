//! Wayland clipboard access, and the session's clipboard history.
//!
//! [`manager`] is the `zwlr_data_control` client: it watches the selection,
//! reads it, and can take ownership to put something back. [`history`] is the
//! pure bookkeeping on top — what is remembered, in what order.
//!
//! Both the portal backend (which bridges the selection to RemoteDesktop
//! sessions) and the shell (which offers the history as a launcher mode) use
//! this, which is why it is its own crate rather than living in either.

pub mod history;
pub mod manager;
pub mod service;

pub use self::{
    history::{Entry, History, Kind, TEXT_MIMES, URI_LIST_MIME, best_mime, is_sensitive},
    manager::{ClipEvent, ClipboardHandle, spawn},
    service::{Clipboard, global, start_global},
};
