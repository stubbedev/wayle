//! The `Mode` trait every launch mode implements.

use async_trait::async_trait;

use crate::item::Item;

/// How an entry was accepted.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ActivateKind {
    /// Plain accept (Enter).
    Default,
    /// Alternate accept (Shift+Enter: run-in-terminal, window alt command).
    Alt,
    /// Accept of custom typed text that matches no row.
    Custom(String),
    /// `kb-custom-N` (1..=19); dmenu/script exit codes 10..=28.
    KbCustom(u8),
}

/// What the surface should do after a mode handled an event.
#[derive(Debug)]
pub enum Action {
    /// Done — hide the surface.
    Close,
    /// Replace the current list/prompt/message with new state.
    Reload(ModeState),
    /// Switch to the named mode (script `switch-mode`).
    SwitchMode(String),
    /// Replace the query text (script `new-selection` companion).
    SetInput(String),
    /// Terminate the session with an exit code and the accepted rows
    /// (dmenu mode only; forwarded to the waiting CLI).
    Exit {
        /// Process exit code (0 accept, 1 cancel, 10..=28 kb-custom-N).
        code: i32,
        /// Accepted rows as `(input index, text)`; index `-1` = custom input.
        selected: Vec<(i64, String)>,
    },
    /// Nothing to do.
    Nothing,
}

/// Everything the surface needs to render a mode's list.
#[derive(Debug, Default)]
pub struct ModeState {
    /// The rows. Vec index = row identity.
    pub items: Vec<Item>,
    /// Prompt text left of the input.
    pub prompt: String,
    /// Optional message row between input and list (may be markup).
    pub message: Option<String>,
    /// Render row text as Pango markup by default.
    pub markup_rows: bool,
    /// Allow toggling multiple rows before accept.
    pub multi_select: bool,
    /// Reject custom (non-row) input.
    pub no_custom: bool,
    /// Route kb-custom-N to the mode instead of closing.
    pub use_hot_keys: bool,
    /// Keep list selection position across a reload (script `keep-selection`).
    pub keep_selection: bool,
    /// Absolute selection position to apply (script `new-selection`).
    pub new_selection: Option<u32>,
    /// Keep the typed filter across a reload (script `keep-filter`).
    pub keep_filter: bool,
}

/// One launch mode (drun, run, window, ssh, script, dmenu, ...).
#[async_trait]
pub trait Mode: Send {
    /// Canonical name ("drun", "run", or a script mode's configured name).
    fn name(&self) -> &str;

    /// Human-facing name (rofi `display-{mode}` override).
    fn display_name(&self) -> &str {
        self.name()
    }

    /// Produce the initial list state.
    async fn load(&mut self) -> ModeState;

    /// Handle acceptance of a row (`Some(index)`) or of custom input
    /// (`None`). `input` is the query text at accept time (`ROFI_INPUT`).
    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, input: &str) -> Action;

    /// Handle a multi-select accept (dmenu). Default: activate the first.
    async fn activate_many(&mut self, indices: &[u32], input: &str) -> Action {
        match indices.first() {
            Some(&first) => self.activate(Some(first), ActivateKind::Default, input).await,
            None => Action::Nothing,
        }
    }

    /// Handle shift-delete on a row (history removal, window close).
    /// Default: unsupported.
    async fn delete(&mut self, _index: u32) -> Action {
        Action::Nothing
    }

    /// Whether typed text that matches no row can be accepted.
    fn allows_custom(&self) -> bool {
        true
    }

    /// Combi only: item mask for a `!bang` prefix (true = shown). `None`
    /// when the bang matches no sub-mode (or the mode isn't combi).
    fn subset(&self, _bang: &str) -> Option<Vec<bool>> {
        None
    }
}
