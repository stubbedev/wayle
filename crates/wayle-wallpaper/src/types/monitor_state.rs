use std::path::PathBuf;

use crate::types::FitMode;

/// Per-monitor wallpaper state.
///
/// Tracks wallpaper path, scaling mode, and cycling position for one monitor.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct MonitorState {
    /// Current wallpaper displayed on this monitor.
    pub wallpaper: Option<PathBuf>,
    /// Image scaling mode for this monitor.
    pub fit_mode: FitMode,
    /// Position in the cycling image list (only used when cycling is active).
    pub cycle_index: usize,
}

impl MonitorState {
    /// Creates a new monitor state with no wallpaper and default fit mode.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Creates a new monitor state with a specific cycle index.
    #[must_use]
    pub fn with_cycle_index(cycle_index: usize) -> Self {
        Self {
            cycle_index,
            ..Self::default()
        }
    }

    /// Advances the cycle index, wrapping around at the given count.
    pub const fn advance(&mut self, image_count: usize) {
        if let Some(rem) = self.cycle_index.saturating_add(1).checked_rem(image_count) {
            self.cycle_index = rem;
        }
    }

    /// Goes back in the cycle, wrapping around at the given count.
    pub const fn previous(&mut self, image_count: usize) {
        if image_count > 0 {
            self.cycle_index = if self.cycle_index == 0 {
                image_count.saturating_sub(1)
            } else {
                self.cycle_index.saturating_sub(1)
            };
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn advance_increments_index() {
        let mut state = MonitorState::with_cycle_index(0);

        state.advance(5);
        assert_eq!(state.cycle_index, 1);

        state.advance(5);
        assert_eq!(state.cycle_index, 2);
    }

    #[test]
    fn advance_wraps_at_count() {
        let mut state = MonitorState::with_cycle_index(4);

        state.advance(5);

        assert_eq!(state.cycle_index, 0);
    }

    #[test]
    fn previous_decrements_index() {
        let mut state = MonitorState::with_cycle_index(3);

        state.previous(5);
        assert_eq!(state.cycle_index, 2);

        state.previous(5);
        assert_eq!(state.cycle_index, 1);
    }

    #[test]
    fn previous_wraps_at_zero() {
        let mut state = MonitorState::with_cycle_index(0);

        state.previous(5);

        assert_eq!(state.cycle_index, 4);
    }

    #[test]
    fn advance_does_nothing_for_zero_count() {
        let mut state = MonitorState::with_cycle_index(0);

        state.advance(0);

        assert_eq!(state.cycle_index, 0);
    }

    #[test]
    fn previous_does_nothing_for_zero_count() {
        let mut state = MonitorState::with_cycle_index(0);

        state.previous(0);

        assert_eq!(state.cycle_index, 0);
    }
}
