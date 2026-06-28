//! `wayle portal` — the xdg-desktop-portal backend plus its sibling tools.
//!
//! Bare `wayle portal` (and `wayle portal run`) launch the long-lived
//! `org.freedesktop.impl.portal.*` backend that the `xdg-desktop-portal`
//! frontend D-Bus-activates. `wayle portal share-picker` is the legacy
//! xdg-desktop-portal-hyprland screencast picker stub, and `wayle portal show`
//! previews individual dialog UIs during development.

/// Portal backend daemon entry point.
pub mod backend;
/// `wayle portal` subcommand definitions.
pub mod commands;
/// xdg-desktop-portal-hyprland screencast picker stub.
pub mod share_picker;
/// `wayle portal show` dialog previewer.
pub mod show;

pub use commands::{PortalCommands, PortalDialog};
