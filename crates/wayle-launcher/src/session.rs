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
    /// Matching engine; the surface reads matches from here.
    pub engine: MatchEngine,
    /// Multi-select accumulation (matched-item indices).
    pub selected: BTreeSet<u32>,
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
            engine: MatchEngine::new(options, notify),
            selected: BTreeSet::new(),
        }
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
        if matches!(kind, ActivateKind::Custom(_)) && !self.modes[self.active].allows_custom() {
            return Action::Nothing;
        }
        let action = self.modes[self.active].activate(index, kind).await;
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

        async fn activate(&mut self, index: Option<u32>, kind: ActivateKind) -> Action {
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
}
