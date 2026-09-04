//! A launcher session: the loaded modes, the active one, and its match state.

use std::{collections::BTreeSet, sync::Arc};

use crate::{
    item::Item,
    matcher::{MatchEngine, MatcherOptions},
    mode::{Action, ActivateKind, Mode, ModeState},
};

/// One open launcher invocation. The surface drives this: forwards query
/// edits, accept/delete/mode-switch keys, and renders
/// [`matched`](MatchEngine::matched) rows.
pub struct Session {
    modes: Vec<Box<dyn Mode>>,
    active: usize,
    state: ModeState,
    /// Current query text (`ROFI_INPUT`; bang prefix already stripped).
    query: String,
    /// Combi `!bang` item mask, when one is active.
    subset: Option<Vec<bool>>,
    /// Matching engine; the surface reads matches from here.
    pub engine: MatchEngine,
    /// Multi-select accumulation (matched-item indices).
    pub selected: BTreeSet<u32>,
    /// `-completer-mode`: the mode `kb-mode-complete` opens to *complete*
    /// the query rather than to launch something.
    completer: Option<String>,
    /// While the completer is open, the mode index to return to.
    completing_from: Option<usize>,
}

impl Session {
    /// Create a session over `modes`, activating the first.
    ///
    /// # Panics
    ///
    /// Panics if `modes` is empty.
    pub fn new(
        modes: Vec<Box<dyn Mode>>,
        options: MatcherOptions,
        notify: Arc<dyn Fn() + Send + Sync>,
    ) -> Self {
        assert!(!modes.is_empty(), "session needs at least one mode");
        Self {
            modes,
            active: 0,
            state: ModeState::default(),
            query: String::new(),
            subset: None,
            engine: MatchEngine::new(options, notify),
            selected: BTreeSet::new(),
            completer: None,
            completing_from: None,
        }
    }

    /// Names the mode `kb-mode-complete` opens (rofi `-completer-mode`).
    ///
    /// It has to be one of the session's own modes; the surface adds it to
    /// the list when the flag names one that is not already there.
    pub fn set_completer(&mut self, mode: Option<String>) {
        self.completer = mode.filter(|name| !name.is_empty());
    }

    /// Whether the completer is currently open.
    #[must_use]
    pub fn completing(&self) -> bool {
        self.completing_from.is_some()
    }

    /// Opens the completer, remembering where to come back to.
    ///
    /// Returns false when no completer is configured or it is not a mode this
    /// session loaded — pressing the key then does nothing rather than
    /// switching to some arbitrary mode.
    pub async fn open_completer(&mut self) -> bool {
        if self.completing_from.is_some() {
            return false;
        }
        let Some(name) = self.completer.clone() else {
            return false;
        };
        let Some(index) = self.modes.iter().position(|mode| mode.name() == name) else {
            return false;
        };
        if index == self.active {
            return false;
        }
        let from = self.active;
        self.switch_to(index).await;
        self.completing_from = Some(from);
        true
    }

    /// Closes the completer without taking anything from it.
    pub async fn cancel_completer(&mut self) {
        if let Some(from) = self.completing_from.take() {
            self.switch_to(from).await;
        }
    }

    /// Takes the completer's row as the query and returns to the mode that
    /// asked for it.
    ///
    /// The point of a completer is that accepting a row *fills the box*
    /// rather than launching it, so the caller gets [`Action::SetInput`]
    /// instead of whatever the completer mode would have done.
    pub async fn complete_with(&mut self, index: Option<u32>) -> Action {
        let Some(from) = self.completing_from.take() else {
            return Action::Nothing;
        };
        let text = index
            .and_then(|index| self.engine.items().get(index as usize))
            .map(|item| item.display.clone());
        self.switch_to(from).await;
        match text {
            Some(text) => Action::SetInput(text),
            None => Action::Nothing,
        }
    }

    /// Update the query. A leading `!bang ` token restricts a combi mode to
    /// the matching sub-mode; the remainder is the real match query.
    pub fn set_query(&mut self, query: &str) {
        let (bang, rest) = split_bang(query);
        self.subset = bang.and_then(|bang| self.modes[self.active].subset(bang));
        self.query = if self.subset.is_some() {
            rest.to_owned()
        } else {
            query.to_owned()
        };
        // Modes that answer the query rather than being searched by it get to
        // rebuild their list first, so the engine matches against the answer.
        if let Some(state) = self.modes[self.active].query(&self.query) {
            self.apply_state(state);
        }
        self.engine.set_query(&self.query);
    }

    /// Ranked matched indices with any combi `!bang` mask applied.
    pub fn matched(&mut self) -> Vec<u32> {
        let mut matched = self.engine.matched();
        if let Some(mask) = &self.subset {
            matched.retain(|&index| mask.get(index as usize).copied().unwrap_or(true));
        }
        matched
    }

    /// Names of all loaded modes (sidebar tabs, kb-mode-next order).
    pub fn mode_names(&self) -> Vec<&str> {
        self.modes.iter().map(|mode| mode.name()).collect()
    }

    /// Display names of all loaded modes.
    pub fn mode_display_names(&self) -> Vec<&str> {
        self.modes.iter().map(|mode| mode.display_name()).collect()
    }

    /// Index of the active mode.
    pub fn active_index(&self) -> usize {
        self.active
    }

    /// State of the active mode (prompt, message, flags).
    pub fn state(&self) -> &ModeState {
        &self.state
    }

    /// Load (or reload) the active mode and feed its items to the engine.
    pub async fn load(&mut self) {
        let state = self.modes[self.active].load().await;
        self.apply_state(state);
    }

    /// Switch to the mode at `index` (wrapping) and load it.
    pub async fn switch_to(&mut self, index: usize) {
        self.active = index % self.modes.len();
        self.selected.clear();
        self.subset = None;
        self.load().await;
    }

    /// Switch to the next mode (kb-mode-next).
    pub async fn switch_next(&mut self) {
        self.switch_to((self.active + 1) % self.modes.len()).await;
    }

    /// Switch to the previous mode (kb-mode-previous).
    pub async fn switch_previous(&mut self) {
        self.switch_to((self.active + self.modes.len() - 1) % self.modes.len())
            .await;
    }

    /// Switch to a mode by name. Returns false if unknown.
    pub async fn switch_to_named(&mut self, name: &str) -> bool {
        // Resolve the position before awaiting: a `match` on the iterator
        // expression would hold the `&self.modes` borrow across the await
        // and force a `Sync` bound on `Mode`.
        let position = self.modes.iter().position(|mode| mode.name() == name);
        if let Some(index) = position {
            self.switch_to(index).await;
            true
        } else {
            false
        }
    }

    /// Forward an accept to the active mode and apply the resulting action.
    /// Returns the action for the surface to interpret (Close/Exit/...).
    pub async fn activate(&mut self, index: Option<u32>, kind: ActivateKind) -> Action {
        // While the completer is open, accepting fills the query instead of
        // launching: that is what makes it a completer and not a mode switch.
        if self.completing() {
            return self.complete_with(index).await;
        }
        if matches!(kind, ActivateKind::Custom(_))
            && (!self.modes[self.active].allows_custom() || self.state.no_custom)
        {
            return Action::Nothing;
        }
        let query = self.query.clone();
        let action = self.modes[self.active].activate(index, kind, &query).await;
        self.apply_action(action).await
    }

    /// Forward a multi-select accept (dmenu).
    pub async fn activate_many(&mut self, indices: &[u32]) -> Action {
        let query = self.query.clone();
        let action = self.modes[self.active].activate_many(indices, &query).await;
        self.apply_action(action).await
    }

    /// Forward a shift-delete to the active mode.
    pub async fn delete(&mut self, index: u32) -> Action {
        let action = self.modes[self.active].delete(index).await;
        self.apply_action(action).await
    }

    /// Resolve internal actions (Reload/SwitchMode), pass the rest through.
    async fn apply_action(&mut self, action: Action) -> Action {
        match action {
            Action::Reload(state) => {
                self.apply_state(state);
                Action::Nothing
            }
            Action::SwitchMode(name) => {
                if self.switch_to_named(&name).await {
                    Action::Nothing
                } else {
                    Action::Close
                }
            }
            other => other,
        }
    }

    fn apply_state(&mut self, mut state: ModeState) {
        let mut items = std::mem::take(&mut state.items);
        // Mode-level markup (script `markup-rows`, dmenu `-markup-rows`)
        // becomes a per-item flag so the row factory has one source of truth.
        if state.markup_rows {
            for item in &mut items {
                item.flags |= crate::item::ItemFlags::MARKUP;
            }
        }
        let items: Arc<Vec<Item>> = Arc::new(items);
        self.state = state;
        self.engine.set_items(items);
    }
}

/// Split a leading `!bang ` prefix off a query: `"!win term"` →
/// `(Some("win"), "term")`. A lone `"!win"` (no space yet) also counts.
fn split_bang(query: &str) -> (Option<&str>, &str) {
    let Some(rest) = query.strip_prefix('!') else {
        return (None, query);
    };
    match rest.split_once(char::is_whitespace) {
        Some((bang, remainder)) => (Some(bang), remainder),
        None => (Some(rest), ""),
    }
}

#[cfg(test)]
mod tests {
    use async_trait::async_trait;

    use super::*;
    use crate::item::Item;

    struct StaticMode {
        name: &'static str,
        entries: Vec<&'static str>,
        activated: Option<(Option<u32>, ActivateKind)>,
    }

    #[async_trait]
    impl Mode for StaticMode {
        fn name(&self) -> &str {
            self.name
        }

        async fn load(&mut self) -> ModeState {
            ModeState {
                items: self.entries.iter().map(|entry| Item::new(*entry)).collect(),
                prompt: self.name.to_owned(),
                ..ModeState::default()
            }
        }

        async fn activate(
            &mut self,
            index: Option<u32>,
            kind: ActivateKind,
            _input: &str,
        ) -> Action {
            self.activated = Some((index, kind));
            Action::Close
        }
    }

    fn session() -> Session {
        Session::new(
            vec![
                Box::new(StaticMode {
                    name: "alpha",
                    entries: vec!["a1", "a2"],
                    activated: None,
                }),
                Box::new(StaticMode {
                    name: "beta",
                    entries: vec!["b1"],
                    activated: None,
                }),
            ],
            MatcherOptions::default(),
            Arc::new(|| {}),
        )
    }

    #[tokio::test]
    async fn load_populates_engine_and_prompt() {
        let mut session = session();
        session.load().await;
        assert_eq!(session.state().prompt, "alpha");
        assert_eq!(session.engine.items().len(), 2);
    }

    #[tokio::test]
    async fn mode_switching_wraps_and_loads() {
        let mut session = session();
        session.load().await;
        session.switch_next().await;
        assert_eq!(session.active_index(), 1);
        assert_eq!(session.engine.items().len(), 1);
        session.switch_next().await;
        assert_eq!(session.active_index(), 0);
        session.switch_previous().await;
        assert_eq!(session.active_index(), 1);
    }

    #[tokio::test]
    async fn switch_to_named_unknown_is_false() {
        let mut session = session();
        assert!(!session.switch_to_named("nope").await);
        assert!(session.switch_to_named("beta").await);
        assert_eq!(session.active_index(), 1);
    }

    #[tokio::test]
    async fn activate_passes_through_close() {
        let mut session = session();
        session.load().await;
        let action = session.activate(Some(0), ActivateKind::Default).await;
        assert!(matches!(action, Action::Close));
    }

    #[tokio::test]
    async fn a_completer_fills_the_query_instead_of_launching() {
        let mut session = session();
        session.set_completer(Some(String::from("beta")));
        session.load().await;
        assert_eq!(session.active_index(), 0);

        assert!(session.open_completer().await, "the completer opens");
        assert!(session.completing());
        assert_eq!(session.active_index(), 1, "it is showing the completer");

        // Accepting takes the row's text and returns to where we came from —
        // a completer that launched the row would be a mode switch.
        let action = session.activate(Some(0), ActivateKind::Default).await;
        match action {
            Action::SetInput(text) => assert_eq!(text, "b1"),
            other => panic!("expected the row's text back, got {other:?}"),
        }
        assert!(!session.completing());
        assert_eq!(session.active_index(), 0, "back to the original mode");
    }

    #[tokio::test]
    async fn cancelling_the_completer_takes_nothing_from_it() {
        let mut session = session();
        session.set_completer(Some(String::from("beta")));
        session.load().await;
        assert!(session.open_completer().await);

        session.cancel_completer().await;
        assert!(!session.completing());
        assert_eq!(session.active_index(), 0);
        // And a later accept behaves normally again.
        let action = session.activate(Some(0), ActivateKind::Default).await;
        assert!(matches!(action, Action::Close), "got {action:?}");
    }

    #[tokio::test]
    async fn a_completer_that_is_not_loaded_does_nothing() {
        let mut session = session();
        // Naming a mode this session never loaded must not switch to some
        // arbitrary one.
        session.set_completer(Some(String::from("nope")));
        session.load().await;
        assert!(!session.open_completer().await);
        assert!(!session.completing());
        assert_eq!(session.active_index(), 0);

        // Nor may the completer be the mode already showing: there would be
        // nothing to come back to.
        session.set_completer(Some(String::from("alpha")));
        assert!(!session.open_completer().await);
        assert!(!session.completing());
    }

    #[tokio::test]
    async fn no_completer_configured_means_the_key_is_inert() {
        let mut session = session();
        session.load().await;
        assert!(!session.open_completer().await);
        session.set_completer(Some(String::new()));
        assert!(!session.open_completer().await);
    }

    #[tokio::test]
    async fn the_completer_cannot_be_opened_twice() {
        // Otherwise the second open would overwrite the mode to return to,
        // and the completer would never close.
        let mut session = session();
        session.set_completer(Some(String::from("beta")));
        session.load().await;
        assert!(session.open_completer().await);
        assert!(!session.open_completer().await);
        assert_eq!(session.completing_from, Some(0));
    }
}
