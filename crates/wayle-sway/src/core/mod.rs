//! Reactive wrappers around sway's IPC entities.

mod window;
mod workspace;

pub(crate) use window::WindowSnapshot;
pub use window::Window;
pub use workspace::Workspace;
