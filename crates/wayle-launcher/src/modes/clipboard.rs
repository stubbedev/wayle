//! clipboard mode: the session's clipboard history as a list.
//!
//! This is the mode that replaces the `cliphist list | rofi -dmenu | cliphist
//! decode | wl-copy` pipeline every config carries. wayle already watches the
//! Wayland selection for the portal, so the history is there for the asking —
//! no second daemon, no temp files, and no round trip through a shell
//! pipeline that can only carry text.
//!
//! Accepting a row puts it back on the clipboard under the mime it arrived
//! with, so a copied file goes back as a file and an image as an image rather
//! than as its own filename. `kb-custom-1` forgets one entry and
//! `kb-custom-2` forgets all of them, because a clipboard history is exactly
//! the place a password ends up when a password manager forgets to mark it.

use std::path::PathBuf;

use async_trait::async_trait;
use wayle_clipboard::{Clipboard, Entry, Kind};

use crate::{
    item::{IconSource, Item, ItemFlags},
    mode::{Action, ActivateKind, Mode, ModeState},
};

/// How much of an entry a row shows.
const PREVIEW_CHARS: usize = 160;

/// Row icons, one per [`Kind`].
const ICON_TEXT: &str = "ld-file-text-symbolic";
const ICON_FILES: &str = "ld-folder-symbolic";
const ICON_IMAGE: &str = "ld-image-symbolic";
const ICON_OTHER: &str = "ld-layers-symbolic";

/// `kb-custom-1`: forget the selected entry.
const FORGET_ONE: u8 = 1;

/// `kb-custom-2`: forget the whole history.
const FORGET_ALL: u8 = 2;

/// Clipboard history mode.
pub struct ClipboardMode {
    clipboard: Option<Clipboard>,
    /// Entry ids, in the order the rows are drawn, so a row index resolves to
    /// the entry it was drawn for even after something new arrives.
    ids: Vec<u64>,
}

impl Default for ClipboardMode {
    fn default() -> Self {
        Self::new()
    }
}

impl ClipboardMode {
    /// Creates the mode over the session clipboard.
    #[must_use]
    pub fn new() -> Self {
        Self {
            clipboard: wayle_clipboard::global(),
            ids: Vec::new(),
        }
    }

    /// Creates the mode over a specific clipboard.
    #[must_use]
    pub fn with_clipboard(clipboard: Clipboard) -> Self {
        Self {
            clipboard: Some(clipboard),
            ids: Vec::new(),
        }
    }

    fn state(&mut self) -> ModeState {
        let Some(clipboard) = &self.clipboard else {
            self.ids.clear();
            return ModeState {
                prompt: String::from("clipboard"),
                // Not an empty list: an empty list reads as "nothing copied
                // yet", which is a different and much less useful answer than
                // "this compositor cannot offer a clipboard history".
                message: Some(String::from(
                    "No clipboard history: the compositor does not support wlr-data-control.",
                )),
                no_custom: true,
                use_hot_keys: true,
                ..ModeState::default()
            };
        };

        let entries = clipboard.entries();
        self.ids = entries.iter().map(|entry| entry.id).collect();

        let items = entries
            .iter()
            .map(|entry| Item {
                display: entry.preview(PREVIEW_CHARS),
                match_text: entry.preview(PREVIEW_CHARS),
                info: Some(entry.id.to_string()),
                flags: ItemFlags::empty(),
                icon: Some(icon_for(entry)),
            })
            .collect();

        ModeState {
            items,
            prompt: String::from("clipboard"),
            // Typing something that matches nothing should not put that text
            // on the clipboard: this mode restores, it does not compose.
            no_custom: true,
            use_hot_keys: true,
            ..ModeState::default()
        }
    }

    /// The entry id a row index was drawn for.
    fn id_at(&self, index: Option<u32>) -> Option<u64> {
        self.ids.get(usize::try_from(index?).ok()?).copied()
    }
}

#[async_trait]
impl Mode for ClipboardMode {
    fn name(&self) -> &str {
        "clipboard"
    }

    async fn load(&mut self) -> ModeState {
        self.state()
    }

    async fn activate(&mut self, index: Option<u32>, kind: ActivateKind, _input: &str) -> Action {
        let (Some(clipboard), Some(id)) = (self.clipboard.clone(), self.id_at(index)) else {
            return Action::Nothing;
        };

        match kind {
            ActivateKind::KbCustom(FORGET_ONE) => {
                clipboard.forget(id);
                Action::Reload(self.state())
            }
            ActivateKind::KbCustom(FORGET_ALL) => {
                clipboard.clear();
                Action::Reload(self.state())
            }
            // Every other accept restores, including the alternate one: there
            // is no second thing to do with a clipboard entry.
            _ => {
                clipboard.copy(id);
                Action::Close
            }
        }
    }

    fn allows_custom(&self) -> bool {
        false
    }
}

/// The icon a row gets.
///
/// A single copied file gets a thumbnail of itself — the launcher already
/// generates those for `filebrowser`, and a picture of the picture is the
/// fastest way to tell two copied screenshots apart. Everything else gets a
/// name, including a multi-file copy, which has no one thing to picture.
#[must_use]
fn icon_for(entry: &Entry) -> IconSource {
    let name = match entry.kind() {
        Kind::Text => ICON_TEXT,
        Kind::Files => ICON_FILES,
        Kind::Image => ICON_IMAGE,
        Kind::Other => ICON_OTHER,
    };

    if entry.kind() == Kind::Files {
        let paths = entry.paths();
        if let [only] = paths.as_slice() {
            return IconSource::Thumbnail {
                path: PathBuf::from(only),
                fallback: String::from(name),
            };
        }
    }

    IconSource::Name(String::from(name))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn entry(mime: &str, bytes: &[u8]) -> Entry {
        Entry {
            id: 1,
            mime: mime.to_owned(),
            bytes: bytes.to_vec(),
        }
    }

    fn icon_name(source: &IconSource) -> String {
        match source {
            IconSource::Name(name) => name.clone(),
            IconSource::Thumbnail { fallback, .. } => fallback.clone(),
            IconSource::File(path) => path.display().to_string(),
        }
    }

    #[test]
    fn every_kind_has_its_own_icon() {
        let entries = [
            entry("text/plain;charset=utf-8", b"hi"),
            entry("text/uri-list", b"file:///a\nfile:///b\n"),
            entry("image/png", b"\x89PNG"),
            entry("application/pdf", b"%PDF"),
        ];

        let icons: Vec<String> = entries.iter().map(|e| icon_name(&icon_for(e))).collect();

        let mut unique = icons.clone();
        unique.sort();
        unique.dedup();
        assert_eq!(
            unique.len(),
            icons.len(),
            "two kinds share an icon: {icons:?}"
        );
        assert!(icons.iter().all(|icon| icon.ends_with("-symbolic")));
    }

    #[test]
    fn one_copied_file_is_pictured_and_several_are_not() {
        let one = icon_for(&entry("text/uri-list", b"file:///tmp/a.png\n"));
        let many = icon_for(&entry(
            "text/uri-list",
            b"file:///tmp/a.png\nfile:///tmp/b.png\n",
        ));

        assert!(matches!(
            &one,
            IconSource::Thumbnail { path, fallback }
                if path.as_os_str() == "/tmp/a.png" && fallback == ICON_FILES
        ));
        // Several files have no one thing to picture, so they keep the name.
        assert!(matches!(&many, IconSource::Name(name) if name == ICON_FILES));
    }

    #[tokio::test]
    async fn without_a_clipboard_the_mode_says_so_instead_of_looking_empty() {
        let mut mode = ClipboardMode {
            clipboard: None,
            ids: Vec::new(),
        };

        let state = mode.load().await;

        assert!(state.items.is_empty());
        assert!(
            state
                .message
                .is_some_and(|message| message.contains("wlr-data-control"))
        );
        // Custom text must not become a clipboard entry.
        assert!(state.no_custom);
        assert!(!mode.allows_custom());
    }

    #[tokio::test]
    async fn activating_without_a_clipboard_does_nothing() {
        let mut mode = ClipboardMode {
            clipboard: None,
            ids: Vec::new(),
        };

        let action = mode.activate(Some(0), ActivateKind::Default, "").await;

        assert!(matches!(action, Action::Nothing));
    }

    #[test]
    fn a_row_index_resolves_to_the_entry_it_was_drawn_for() {
        let mode = ClipboardMode {
            clipboard: None,
            ids: vec![7, 4, 1],
        };

        assert_eq!(mode.id_at(Some(0)), Some(7));
        assert_eq!(mode.id_at(Some(2)), Some(1));
        // Past the end, and no row at all, resolve to nothing rather than to
        // whatever happens to be first.
        assert_eq!(mode.id_at(Some(3)), None);
        assert_eq!(mode.id_at(None), None);
    }

    #[test]
    fn the_mode_is_named_for_the_flag_that_selects_it() {
        assert_eq!(ClipboardMode::new().name(), "clipboard");
    }
}
