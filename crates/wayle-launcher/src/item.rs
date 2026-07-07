//! List items produced by modes.

use std::path::PathBuf;

use bitflags::bitflags;

bitflags! {
    /// Row presentation/behavior flags (rofi row metadata).
    #[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
    pub struct ItemFlags: u8 {
        /// Styled urgent (rofi `-u` / row option `urgent`).
        const URGENT = 1 << 0;
        /// Styled active (rofi `-a` / row option `active`).
        const ACTIVE = 1 << 1;
        /// Cannot be activated (row option `nonselectable`).
        const NONSELECTABLE = 1 << 2;
        /// Always shown regardless of filter (row option `permanent`).
        const PERMANENT = 1 << 3;
        /// Display text is Pango markup (`-markup-rows` / `markup-rows`).
        const MARKUP = 1 << 4;
    }
}

/// Where a row icon comes from.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IconSource {
    /// Freedesktop icon-theme name.
    Name(String),
    /// Image file on disk.
    File(PathBuf),
}

/// One list entry. Its index in the mode's item vec is its identity —
/// modes receive that index back on activate/delete.
#[derive(Debug, Clone, Default)]
pub struct Item {
    /// Text shown in the list (may be Pango markup when `MARKUP` is set).
    pub display: String,
    /// Text the matcher sees: display plus invisible `meta` keywords.
    pub match_text: String,
    /// Optional row icon.
    pub icon: Option<IconSource>,
    /// Opaque per-row data handed back on selection (`ROFI_INFO`).
    pub info: Option<String>,
    /// Presentation/behavior flags.
    pub flags: ItemFlags,
}

impl Item {
    /// Plain item: display text doubles as match text.
    pub fn new(display: impl Into<String>) -> Self {
        let display = display.into();
        Self {
            match_text: display.clone(),
            display,
            ..Self::default()
        }
    }
}
