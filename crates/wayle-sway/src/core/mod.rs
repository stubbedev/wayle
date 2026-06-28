//! Reactive wrappers around sway's IPC entities.

mod window;
mod workspace;

pub use window::Window;
pub(crate) use window::WindowSnapshot;
pub use workspace::Workspace;
