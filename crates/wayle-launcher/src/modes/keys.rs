//! keys mode: searchable list of the launcher's own keybindings.

use async_trait::async_trait;

use crate::{
    item::Item,
    mode::{Action, ActivateKind, Mode, ModeState},
};

/// Keybinding list mode. Read-only: rows are informational.
pub struct KeysMode {
    bindings: Vec<(String, String)>,
}

impl KeysMode {
    /// Create from the session's effective bindings.
    #[must_use]
    pub fn new(bindings: Vec<(String, String)>) -> Self {
        Self { bindings }
    }
}

#[async_trait]
impl Mode for KeysMode {
    fn name(&self) -> &str {
        "keys"
    }

    async fn load(&mut self) -> ModeState {
        let items = self
            .bindings
            .iter()
            .map(|(action, keys)| {
                let mut item = Item::new(format!("kb-{action}: {keys}"));
                item.flags |= crate::item::ItemFlags::NONSELECTABLE;
                item
            })
            .collect();
        ModeState {
            items,
            prompt: "keys".to_owned(),
            no_custom: true,
            ..ModeState::default()
        }
    }

    async fn activate(&mut self, _index: Option<u32>, _kind: ActivateKind, _input: &str) -> Action {
        Action::Nothing
    }

    fn allows_custom(&self) -> bool {
        false
    }
}
