//! Launcher engine: the rofi-replacement core.
//!
//! Pure logic — no GTK. Hosts the [`Mode`](mode::Mode) trait and its
//! implementations, the matching/ranking engine, run history/frecency,
//! and the [`Session`](session::Session) that ties them together. The
//! surface (UI) lives in `wayle-shell`.

pub mod error;
pub mod history;
pub mod hooks;
pub mod item;
pub mod keybinds;
pub mod matcher;
pub mod mode;
pub mod modes;
pub mod mouse;
pub mod session;
pub mod spawn;
pub mod template;
pub mod thumbnail;

pub use error::Error;
pub use hooks::Hooks;
pub use item::{IconSource, Item, ItemFlags};
pub use matcher::{CaseMode, MatchEngine, MatchMethod, MatcherOptions, SortMethod, TickStatus};
pub use mode::{Action, ActivateKind, Mode, ModeState};
pub use mouse::{MouseBinding, MouseButton, MouseInput, MouseModifiers, ScrollDirection};
pub use session::Session;
pub use thumbnail::Thumbnailer;
