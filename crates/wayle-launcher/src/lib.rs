//! Launcher engine: the rofi-replacement core.
//!
//! Pure logic — no GTK. Hosts the [`Mode`](mode::Mode) trait and its
//! implementations, the matching/ranking engine, run history/frecency,
//! and the [`Session`](session::Session) that ties them together. The
//! surface (UI) lives in `wayle-shell`.

pub mod error;
pub mod history;
pub mod item;
pub mod keybinds;
pub mod matcher;
pub mod mode;
pub mod modes;
pub mod session;
pub mod spawn;
pub mod template;

pub use error::Error;
pub use item::{IconSource, Item, ItemFlags};
pub use matcher::{CaseMode, MatchEngine, MatchMethod, MatcherOptions, SortMethod};
pub use mode::{Action, ActivateKind, Mode, ModeState};
pub use session::Session;
