//! dmenu mode: rows from the CLI, selection back to the CLI.

use async_trait::async_trait;
use tokio::sync::mpsc;

use crate::{
    item::ItemFlags,
    mode::{Action, ActivateKind, Mode, ModeState},
    modes::script::{parse_ranges, parse_row},
};

/// dmenu behavior knobs (subset of the CLI's dmenu flags the engine needs).
#[derive(Debug, Clone, Default)]
pub struct DmenuConfig {
    /// `-p` prompt.
    pub prompt: Option<String>,
    /// `-mesg` message row.
    pub message: Option<String>,
    /// `-markup-rows`.
    pub markup_rows: bool,
    /// `-multi-select`.
    pub multi_select: bool,
    /// `-no-custom` / `-only-match`.
    pub no_custom: bool,
    /// `-u` urgent indices or ranges (pre-expanded), as a raw spec string.
    pub urgent: Vec<u32>,
    /// `-a` active indices.
    pub active: Vec<u32>,
}

/// dmenu mode: list arrives over a channel, accept exits the session.
pub struct DmenuMode {
    config: DmenuConfig,
    rows: Option<mpsc::Receiver<Vec<String>>>,
    texts: Vec<String>,
}

impl DmenuMode {
    /// Create the mode over the CLI's row stream.
    #[must_use]
    pub fn new(config: DmenuConfig, rows: mpsc::Receiver<Vec<String>>) -> Self {
        Self {
            config,
            rows: Some(rows),
            texts: Vec::new(),
        }
    }

    fn selected(&self, index: u32) -> Option<(i64, String)> {
        self.texts
            .get(index as usize)
            .map(|text| (i64::from(index), text.clone()))
    }
}

#[async_trait]
impl Mode for DmenuMode {
    fn name(&self) -> &str {
        "dmenu"
    }

    async fn load(&mut self) -> ModeState {
        // Drain the whole row stream before building the list.
        // ponytail: rows land as one batch; stream chunks into the matcher
        // incrementally if `tail -f | wayle launcher -dmenu` ever matters.
        let mut texts = Vec::new();
        let mut items = Vec::new();
        if let Some(mut rows) = self.rows.take() {
            while let Some(chunk) = rows.recv().await {
                for raw in chunk {
                    let parsed = parse_row(&raw);
                    texts.push(parsed.text);
                    items.push(parsed.item);
                }
            }
        }
        for &index in &self.config.urgent {
            if let Some(item) = items.get_mut(index as usize) {
                item.flags |= ItemFlags::URGENT;
            }
        }
        for &index in &self.config.active {
            if let Some(item) = items.get_mut(index as usize) {
                item.flags |= ItemFlags::ACTIVE;
            }
        }
        self.texts = texts;
        ModeState {
            items,
            prompt: self
                .config
                .prompt
                .clone()
                .unwrap_or_else(|| "dmenu".to_owned()),
            message: self.config.message.clone(),
            markup_rows: self.config.markup_rows,
            multi_select: self.config.multi_select,
            no_custom: self.config.no_custom,
            use_hot_keys: true, // kb-custom-N always exits with 10..=28
            ..ModeState::default()
        }
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, input: &str) -> Action {
        let code = match &kind {
            ActivateKind::KbCustom(n) => 9 + i32::from(*n),
            _ => 0,
        };
        let selected = match (&kind, index) {
            (ActivateKind::Custom(custom), _) => vec![(-1, custom.clone())],
            (_, Some(row)) => self.selected(row).into_iter().collect(),
            (_, None) => vec![(-1, input.to_owned())],
        };
        if selected.is_empty() {
            return Action::Nothing;
        }
        Action::Exit { code, selected }
    }

    async fn activate_many(&mut self, indices: &[u32], _input: &str) -> Action {
        let selected: Vec<(i64, String)> = indices
            .iter()
            .filter_map(|&index| self.selected(index))
            .collect();
        if selected.is_empty() {
            return Action::Nothing;
        }
        Action::Exit { code: 0, selected }
    }
}

/// Re-export for the CLI-side range parsing (`-u 1,3-5`).
#[must_use]
pub fn expand_ranges(raw: &str) -> Vec<u32> {
    parse_ranges(raw)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mode_with_rows(rows: Vec<&str>, config: DmenuConfig) -> DmenuMode {
        let (tx, rx) = mpsc::channel(4);
        let rows: Vec<String> = rows.into_iter().map(ToOwned::to_owned).collect();
        tokio::spawn(async move {
            let _ = tx.send(rows).await;
        });
        DmenuMode::new(config, rx)
    }

    #[tokio::test]
    async fn rows_become_items_with_flags() {
        let config = DmenuConfig {
            urgent: vec![0],
            ..DmenuConfig::default()
        };
        let mut mode = mode_with_rows(vec!["alpha", "beta\0icon\u{1f}firefox"], config);
        let state = mode.load().await;
        assert_eq!(state.items.len(), 2);
        assert!(state.items[0].flags.contains(ItemFlags::URGENT));
        assert!(state.items[1].icon.is_some());
    }

    #[tokio::test]
    async fn accept_exits_with_index_and_text() {
        let mut mode = mode_with_rows(vec!["alpha", "beta"], DmenuConfig::default());
        let _ = mode.load().await;
        let action = mode.activate(Some(1), ActivateKind::Default, "be").await;
        let Action::Exit { code, selected } = action else {
            unreachable!("expected exit");
        };
        assert_eq!(code, 0);
        assert_eq!(selected, vec![(1, "beta".to_owned())]);
    }

    #[tokio::test]
    async fn kb_custom_maps_to_rofi_codes() {
        let mut mode = mode_with_rows(vec!["alpha"], DmenuConfig::default());
        let _ = mode.load().await;
        let action = mode.activate(Some(0), ActivateKind::KbCustom(3), "").await;
        let Action::Exit { code, .. } = action else {
            unreachable!("expected exit");
        };
        assert_eq!(code, 12);
    }

    #[tokio::test]
    async fn multi_select_exit_carries_all() {
        let mut mode = mode_with_rows(vec!["a", "b", "c"], DmenuConfig::default());
        let _ = mode.load().await;
        let action = mode.activate_many(&[0, 2], "").await;
        let Action::Exit { selected, .. } = action else {
            unreachable!("expected exit");
        };
        assert_eq!(selected, vec![(0, "a".to_owned()), (2, "c".to_owned())]);
    }
}
