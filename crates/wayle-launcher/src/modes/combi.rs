//! combi mode: several modes merged into one list, `!bang` filterable.

use async_trait::async_trait;

use crate::{
    item::Item,
    mode::{Action, ActivateKind, Mode, ModeState},
    template,
};

/// Combined-modes mode.
pub struct CombiMode {
    children: Vec<Box<dyn Mode>>,
    /// Row template (`{mode}`, `{text}`).
    display_format: String,
    /// Child index per merged item index.
    owners: Vec<usize>,
    /// First local index per merged item index.
    locals: Vec<u32>,
}

impl CombiMode {
    /// Create over child modes (rofi `-combi-modes`).
    #[must_use]
    pub fn new(children: Vec<Box<dyn Mode>>, display_format: String) -> Self {
        Self {
            children,
            display_format,
            owners: Vec::new(),
            locals: Vec::new(),
        }
    }

    fn merge(&mut self, states: Vec<ModeState>) -> ModeState {
        let mut items: Vec<Item> = Vec::new();
        let mut owners = Vec::new();
        let mut locals = Vec::new();
        for (child_index, state) in states.into_iter().enumerate() {
            let child_name = self.children[child_index].display_name().to_owned();
            for (local_index, mut item) in state.items.into_iter().enumerate() {
                if self.display_format != "{text}" {
                    item.display = template::render(&self.display_format, |key| match key {
                        "mode" => Some(child_name.clone()),
                        "text" => Some(item.display.clone()),
                        _ => None,
                    });
                }
                items.push(item);
                owners.push(child_index);
                locals.push(u32::try_from(local_index).unwrap_or(u32::MAX));
            }
        }
        self.owners = owners;
        self.locals = locals;
        ModeState {
            items,
            prompt: "combi".to_owned(),
            ..ModeState::default()
        }
    }

    fn locate(&self, index: u32) -> Option<(usize, u32)> {
        Some((
            *self.owners.get(index as usize)?,
            *self.locals.get(index as usize)?,
        ))
    }

    /// Re-merge after a child changed (its Reload action).
    async fn reload_all(&mut self) -> ModeState {
        let mut states = Vec::with_capacity(self.children.len());
        for child in &mut self.children {
            states.push(child.load().await);
        }
        self.merge(states)
    }
}

#[async_trait]
impl Mode for CombiMode {
    fn name(&self) -> &str {
        "combi"
    }

    async fn load(&mut self) -> ModeState {
        self.reload_all().await
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, input: &str) -> Action {
        match index.and_then(|merged| self.locate(merged)) {
            Some((child, local)) => {
                let action = self.children[child]
                    .activate(Some(local), kind, input)
                    .await;
                self.forward(action).await
            }
            None => {
                // Custom input goes to the first child that accepts it.
                let position = self.children.iter().position(|child| child.allows_custom());
                let Some(child) = position else {
                    return Action::Nothing;
                };
                let action = self.children[child].activate(None, kind, input).await;
                self.forward(action).await
            }
        }
    }

    async fn delete(&mut self, index: u32) -> Action {
        match self.locate(index) {
            Some((child, local)) => {
                let action = self.children[child].delete(local).await;
                self.forward(action).await
            }
            None => Action::Nothing,
        }
    }

    /// Item mask for a `!bang`: children whose name starts with the bang.
    fn subset(&self, bang: &str) -> Option<Vec<bool>> {
        if bang.is_empty() {
            return None;
        }
        let allowed: Vec<bool> = self
            .children
            .iter()
            .map(|child| child.name().starts_with(bang))
            .collect();
        if !allowed.iter().any(|&hit| hit) {
            return None;
        }
        Some(self.owners.iter().map(|&owner| allowed[owner]).collect())
    }
}

impl CombiMode {
    /// A child Reload must re-merge the whole combined list.
    async fn forward(&mut self, action: Action) -> Action {
        match action {
            Action::Reload(_) => Action::Reload(self.reload_all().await),
            other => other,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::mode::ModeState;

    struct StaticChild {
        name: &'static str,
        entries: Vec<&'static str>,
    }

    #[async_trait]
    impl Mode for StaticChild {
        fn name(&self) -> &str {
            self.name
        }

        async fn load(&mut self) -> ModeState {
            ModeState {
                items: self.entries.iter().map(|entry| Item::new(*entry)).collect(),
                ..ModeState::default()
            }
        }

        async fn activate(
            &mut self,
            _index: Option<u32>,
            _kind: ActivateKind,
            _input: &str,
        ) -> Action {
            Action::Close
        }
    }

    fn combi() -> CombiMode {
        CombiMode::new(
            vec![
                Box::new(StaticChild {
                    name: "window",
                    entries: vec!["term", "browser"],
                }),
                Box::new(StaticChild {
                    name: "drun",
                    entries: vec!["Firefox"],
                }),
            ],
            "{mode} {text}".to_owned(),
        )
    }

    #[tokio::test]
    async fn merges_children_with_format() {
        let mut combi = combi();
        let state = combi.load().await;
        assert_eq!(state.items.len(), 3);
        assert_eq!(state.items[0].display, "window term");
        assert_eq!(state.items[2].display, "drun Firefox");
    }

    #[tokio::test]
    async fn bang_subset_masks_other_children() {
        let mut combi = combi();
        let _ = combi.load().await;
        let mask = combi.subset("dr").unwrap();
        assert_eq!(mask, vec![false, false, true]);
        assert!(combi.subset("xyz").is_none());
    }

    #[tokio::test]
    async fn activation_routes_to_owner() {
        let mut combi = combi();
        let _ = combi.load().await;
        let action = combi.activate(Some(2), ActivateKind::Default, "").await;
        assert!(matches!(action, Action::Close));
    }
}
