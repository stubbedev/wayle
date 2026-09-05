//! Session clipboard history.
//!
//! Pure logic: what gets remembered, how it is labelled, and what is refused.
//! The Wayland side that feeds it lives in [`crate::manager`].
//!
//! An entry is bytes plus the mime type they arrived under, not a string. A
//! file manager puts `text/uri-list` on the clipboard, an image editor
//! `image/png`, a terminal `text/plain;charset=utf-8` — remembering only the
//! last of those would make the history useless for exactly the things that
//! are most annoying to copy twice.

use std::collections::VecDeque;

/// How many entries are kept before the oldest is dropped.
pub const DEFAULT_CAPACITY: usize = 200;

/// The largest selection worth remembering, in bytes.
///
/// The history lives in memory for the session, and a copied image or a large
/// file list is unbounded. Past this the entry is refused rather than
/// truncated: half a payload pasted back is worse than not offering it.
pub const MAX_ENTRY_BYTES: usize = 8 * 1024 * 1024;

/// The mime types a plain-text selection arrives under, best first.
pub const TEXT_MIMES: &[&str] = &[
    "text/plain;charset=utf-8",
    "text/plain",
    "UTF8_STRING",
    "STRING",
    "TEXT",
];

/// The mime type a file manager copies files under.
pub const URI_LIST_MIME: &str = "text/uri-list";

/// Mime types wayle will not remember, however they are offered.
///
/// Password managers mark a selection with one of these to ask clipboard
/// managers not to keep it, and honouring that is the difference between a
/// convenience and a credential leak. `x-kde-passwordManagerHint` carries the
/// value `secret`; the others are presence-only conventions.
pub const SENSITIVE_MIMES: &[&str] = &[
    "x-kde-passwordManagerHint",
    "org.kde.passwordManagerHint",
    "password",
    "x-wayle-no-history",
];

/// What a remembered selection is, for labelling and for choosing an icon.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    /// Plain text.
    Text,
    /// One or more `file://` URIs — what copying in a file manager puts down.
    Files,
    /// An image, in whatever encoding the mime names.
    Image,
    /// Anything else, kept verbatim and offered back under its own mime.
    Other,
}

/// One remembered selection.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    /// Stable for the life of the entry, so a row the launcher drew still
    /// names the same selection once something new arrives at the front.
    pub id: u64,
    /// The mime type the bytes are in, and the one they are offered back
    /// under.
    pub mime: String,
    /// The selection, exactly as it was copied.
    pub bytes: Vec<u8>,
}

impl Entry {
    /// What this entry is, from its mime type.
    #[must_use]
    pub fn kind(&self) -> Kind {
        if self.mime == URI_LIST_MIME {
            Kind::Files
        } else if self.mime.starts_with("image/") {
            Kind::Image
        } else if is_text_mime(&self.mime) {
            Kind::Text
        } else {
            Kind::Other
        }
    }

    /// The entry as text, when it is text.
    #[must_use]
    pub fn text(&self) -> Option<String> {
        matches!(self.kind(), Kind::Text | Kind::Files)
            .then(|| String::from_utf8(self.bytes.clone()).ok())
            .flatten()
    }

    /// The paths in a `text/uri-list` entry.
    ///
    /// Comment lines are part of the format (RFC 2483) and are not paths, and
    /// a URI that is not `file://` has no path to give.
    #[must_use]
    pub fn paths(&self) -> Vec<String> {
        if self.kind() != Kind::Files {
            return Vec::new();
        }
        let Ok(text) = std::str::from_utf8(&self.bytes) else {
            return Vec::new();
        };

        text.lines()
            .map(str::trim)
            .filter(|line| !line.is_empty() && !line.starts_with('#'))
            .filter_map(|line| line.strip_prefix("file://").map(percent_decode))
            .collect()
    }

    /// A single line for a list row.
    #[must_use]
    pub fn preview(&self, max_chars: usize) -> String {
        let label = match self.kind() {
            // Runs of whitespace — newlines included — collapse to one space,
            // so a copied paragraph is one readable row rather than something
            // that breaks the list's geometry.
            Kind::Text => self
                .text()
                .unwrap_or_default()
                .split_whitespace()
                .collect::<Vec<&str>>()
                .join(" "),
            Kind::Files => {
                let paths = self.paths();
                let names: Vec<&str> = paths
                    .iter()
                    .map(|path| path.rsplit('/').next().unwrap_or(path.as_str()))
                    .collect();
                match names.len() {
                    0 => String::from("No files"),
                    1 => names.join(", "),
                    count => format!("{} ({count} files)", names.join(", ")),
                }
            }
            Kind::Image => format!("Image · {} · {}", self.mime, human_size(self.bytes.len())),
            Kind::Other => format!("{} · {}", self.mime, human_size(self.bytes.len())),
        };

        truncate_chars(&label, max_chars)
    }
}

/// Whether a mime type names plain text.
#[must_use]
pub fn is_text_mime(mime: &str) -> bool {
    TEXT_MIMES.contains(&mime) || mime.starts_with("text/")
}

/// Whether a selection offering these mimes must not be remembered.
#[must_use]
pub fn is_sensitive(mimes: &[String]) -> bool {
    mimes
        .iter()
        .any(|mime| SENSITIVE_MIMES.iter().any(|hint| mime == hint))
}

/// Picks the mime worth remembering out of everything the owner offered.
///
/// Files beat images beat text: a file manager also offers the file's name as
/// `text/plain`, and remembering that instead would put a bare filename on the
/// clipboard where a file was copied. Within text, the preference order of
/// [`TEXT_MIMES`] wins so the entry is UTF-8 wherever the owner can provide
/// it.
#[must_use]
pub fn best_mime(mimes: &[String]) -> Option<String> {
    let has = |wanted: &str| mimes.iter().find(|mime| *mime == wanted).cloned();

    has(URI_LIST_MIME)
        .or_else(|| {
            mimes
                .iter()
                .find(|mime| mime.starts_with("image/"))
                .cloned()
        })
        .or_else(|| TEXT_MIMES.iter().find_map(|text| has(text)))
        .or_else(|| mimes.iter().find(|mime| mime.starts_with("text/")).cloned())
        // Something rather than nothing: an unknown selection is still worth
        // being able to paste back, byte for byte.
        .or_else(|| mimes.first().cloned())
}

/// Most-recent-first clipboard history, bounded in both directions.
#[derive(Debug)]
pub struct History {
    entries: VecDeque<Entry>,
    capacity: usize,
    max_entry_bytes: usize,
    next_id: u64,
}

impl Default for History {
    fn default() -> Self {
        Self::new(DEFAULT_CAPACITY, MAX_ENTRY_BYTES)
    }
}

impl History {
    /// Builds an empty history.
    #[must_use]
    pub fn new(capacity: usize, max_entry_bytes: usize) -> Self {
        Self {
            entries: VecDeque::new(),
            capacity,
            max_entry_bytes,
            next_id: 1,
        }
    }

    /// Records a selection, returning the id it was stored under.
    ///
    /// Re-copying something already remembered moves it to the front and keeps
    /// its id rather than adding a second copy: the clipboard is a stack of
    /// *what you have*, and the same bytes twice is one thing.
    ///
    /// Returns `None` when the selection is not worth remembering — empty,
    /// oversized, or marked sensitive by its owner.
    pub fn push(&mut self, mime: &str, bytes: &[u8]) -> Option<u64> {
        if bytes.is_empty() || bytes.len() > self.max_entry_bytes || self.capacity == 0 {
            return None;
        }
        // Whitespace-only text comes from a stray drag as often as from
        // intent, and is unrecognisable as a row. Non-text is judged by
        // length alone: bytes that look like whitespace are still content.
        if is_text_mime(mime) && std::str::from_utf8(bytes).is_ok_and(|text| text.trim().is_empty())
        {
            return None;
        }

        if let Some(index) = self
            .entries
            .iter()
            .position(|entry| entry.mime == mime && entry.bytes == bytes)
        {
            let id = self.entries.get(index).map(|entry| entry.id)?;
            if index > 0
                && let Some(entry) = self.entries.remove(index)
            {
                self.entries.push_front(entry);
            }
            return Some(id);
        }

        let id = self.next_id;
        self.entries.push_front(Entry {
            id,
            mime: mime.to_owned(),
            bytes: bytes.to_vec(),
        });
        self.next_id += 1;

        while self.entries.len() > self.capacity {
            self.entries.pop_back();
        }
        Some(id)
    }

    /// Every entry, most recent first.
    pub fn entries(&self) -> impl Iterator<Item = &Entry> {
        self.entries.iter()
    }

    /// The entry with this id, if it has not aged out.
    #[must_use]
    pub fn get(&self, id: u64) -> Option<&Entry> {
        self.entries.iter().find(|entry| entry.id == id)
    }

    /// Forgets one entry, returning whether it was there.
    pub fn remove(&mut self, id: u64) -> bool {
        let before = self.entries.len();
        self.entries.retain(|entry| entry.id != id);
        self.entries.len() != before
    }

    /// Forgets everything.
    pub fn clear(&mut self) {
        self.entries.clear();
    }

    /// How many entries are held.
    #[must_use]
    pub fn len(&self) -> usize {
        self.entries.len()
    }

    /// Whether nothing is held.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }
}

/// Cuts to `max_chars` characters, not bytes: a byte-wise cut panics on a
/// multi-byte boundary.
fn truncate_chars(text: &str, max_chars: usize) -> String {
    if text.chars().count() <= max_chars {
        return text.to_owned();
    }
    text.chars()
        .take(max_chars.saturating_sub(1))
        .chain(std::iter::once('…'))
        .collect()
}

/// Turns `%20` and friends back into bytes.
///
/// URIs on the clipboard are percent-encoded, so a file called `my photo.png`
/// arrives as `my%20photo.png` and would otherwise be shown, and opened, under
/// the wrong name.
fn percent_decode(value: &str) -> String {
    let mut out: Vec<u8> = Vec::with_capacity(value.len());
    let mut bytes = value.bytes();

    while let Some(byte) = bytes.next() {
        if byte != b'%' {
            out.push(byte);
            continue;
        }
        let hex: String = bytes.by_ref().take(2).map(char::from).collect();
        match u8::from_str_radix(&hex, 16) {
            Ok(decoded) => out.push(decoded),
            // Not an escape after all; keep it as it was rather than dropping
            // characters out of a path.
            Err(_) => {
                out.push(b'%');
                out.extend_from_slice(hex.as_bytes());
            }
        }
    }

    String::from_utf8_lossy(&out).into_owned()
}

/// A byte count as something short enough for a list row.
fn human_size(bytes: usize) -> String {
    const UNITS: [&str; 4] = ["B", "KB", "MB", "GB"];
    #[allow(clippy::cast_precision_loss)]
    let mut size = bytes as f64;
    let mut unit = 0;

    while size >= 1024.0 && unit < UNITS.len() - 1 {
        size /= 1024.0;
        unit += 1;
    }

    if unit == 0 {
        format!("{bytes} B")
    } else {
        format!("{size:.1} {}", UNITS[unit])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const TEXT: &str = "text/plain;charset=utf-8";

    fn mimes(list: &[&str]) -> Vec<String> {
        list.iter().map(|mime| (*mime).to_owned()).collect()
    }

    fn entry(mime: &str, bytes: &[u8]) -> Entry {
        Entry {
            id: 1,
            mime: mime.to_owned(),
            bytes: bytes.to_vec(),
        }
    }

    #[test]
    fn newest_selection_comes_first() {
        let mut history = History::default();

        assert!(history.push(TEXT, b"one").is_some());
        assert!(history.push(TEXT, b"two").is_some());

        let texts: Vec<String> = history.entries().filter_map(Entry::text).collect();
        assert_eq!(texts, ["two", "one"]);
    }

    #[test]
    fn recopying_moves_the_entry_without_duplicating_it() {
        let mut history = History::default();
        let first = history.push(TEXT, b"one");
        history.push(TEXT, b"two");

        assert_eq!(history.push(TEXT, b"one"), first);
        assert_eq!(history.len(), 2);
        assert_eq!(history.entries().next().map(|entry| entry.id), first);
    }

    #[test]
    fn the_same_bytes_under_a_different_mime_is_a_different_entry() {
        let mut history = History::default();

        history.push(TEXT, b"data");
        history.push("image/png", b"data");

        // Pasting a PNG back as text/plain would hand the receiver bytes it
        // cannot use, so they cannot share an entry.
        assert_eq!(history.len(), 2);
    }

    #[test]
    fn the_oldest_entry_falls_off_the_end() {
        let mut history = History::new(2, MAX_ENTRY_BYTES);

        history.push(TEXT, b"one");
        history.push(TEXT, b"two");
        history.push(TEXT, b"three");

        let texts: Vec<String> = history.entries().filter_map(Entry::text).collect();
        assert_eq!(texts, ["three", "two"]);
    }

    #[test]
    fn selections_that_are_not_worth_remembering_are_refused() {
        let mut history = History::new(4, 8);

        assert!(history.push(TEXT, b"").is_none());
        assert!(history.push(TEXT, b"   \n\t ").is_none());
        assert!(history.push(TEXT, b"123456789").is_none());
        assert!(history.is_empty());

        // Exactly at the cap is fine, and whitespace-looking *binary* is
        // content rather than a stray drag.
        assert!(history.push(TEXT, b"12345678").is_some());
        assert!(history.push("image/png", b"  \n  ").is_some());
        assert_eq!(history.len(), 2);
    }

    #[test]
    fn a_zero_capacity_history_remembers_nothing() {
        let mut history = History::new(0, MAX_ENTRY_BYTES);

        assert!(history.push(TEXT, b"one").is_none());
        assert!(history.is_empty());
    }

    #[test]
    fn an_aged_out_or_removed_id_resolves_to_nothing() {
        let mut history = History::new(1, MAX_ENTRY_BYTES);
        let old = history.push(TEXT, b"one").unwrap_or(0);
        let new = history.push(TEXT, b"two").unwrap_or(0);

        assert!(history.get(old).is_none());
        assert!(history.get(u64::MAX).is_none());
        assert!(history.remove(new));
        assert!(!history.remove(new));
        assert!(history.is_empty());
    }

    #[test]
    fn a_password_manager_hint_keeps_the_secret_out_of_the_history() {
        let sensitive = mimes(&[TEXT, "x-kde-passwordManagerHint"]);

        assert!(is_sensitive(&sensitive));
        // The ordinary case must not be swept up with it.
        assert!(!is_sensitive(&mimes(&[TEXT, "text/html"])));
        assert!(!is_sensitive(&[]));
    }

    #[test]
    fn files_beat_the_filename_text_offered_alongside_them() {
        // What a file manager actually offers when you copy one file.
        let offered = mimes(&[TEXT, "text/plain", URI_LIST_MIME, "text/html"]);

        assert_eq!(best_mime(&offered).as_deref(), Some(URI_LIST_MIME));
    }

    #[test]
    fn an_image_beats_text_but_files_beat_an_image() {
        assert_eq!(
            best_mime(&mimes(&["text/plain", "image/png"])).as_deref(),
            Some("image/png")
        );
        assert_eq!(
            best_mime(&mimes(&["image/png", URI_LIST_MIME])).as_deref(),
            Some(URI_LIST_MIME)
        );
    }

    #[test]
    fn utf8_text_is_preferred_over_the_older_text_flavours() {
        assert_eq!(
            best_mime(&mimes(&["STRING", "TEXT", TEXT])).as_deref(),
            Some(TEXT)
        );
        assert_eq!(
            best_mime(&mimes(&["STRING", "TEXT"])).as_deref(),
            Some("STRING")
        );
    }

    #[test]
    fn an_unknown_selection_is_still_kept_and_nothing_offered_is_none() {
        assert_eq!(
            best_mime(&mimes(&["application/x-thing"])).as_deref(),
            Some("application/x-thing")
        );
        assert_eq!(best_mime(&[]), None);
    }

    #[test]
    fn kinds_come_from_the_mime() {
        assert_eq!(entry(TEXT, b"hi").kind(), Kind::Text);
        assert_eq!(entry("text/html", b"<p>").kind(), Kind::Text);
        assert_eq!(entry(URI_LIST_MIME, b"file:///a").kind(), Kind::Files);
        assert_eq!(entry("image/png", b"\x89PNG").kind(), Kind::Image);
        assert_eq!(entry("application/pdf", b"%PDF").kind(), Kind::Other);
    }

    #[test]
    fn uri_list_yields_decoded_paths() {
        let list = entry(
            URI_LIST_MIME,
            b"# a comment\r\nfile:///home/me/my%20photo.png\r\nfile:///tmp/notes.txt\r\n",
        );

        assert_eq!(list.paths(), ["/home/me/my photo.png", "/tmp/notes.txt"]);
    }

    #[test]
    fn a_non_file_uri_and_a_non_list_entry_yield_no_paths() {
        // Only `file://` URIs name something on disk.
        assert!(
            entry(URI_LIST_MIME, b"https://example.com/a\n")
                .paths()
                .is_empty()
        );
        // Text that happens to look like one is still text.
        assert!(entry(TEXT, b"file:///tmp/a").paths().is_empty());
    }

    #[test]
    fn a_stray_percent_in_a_name_survives_decoding() {
        let list = entry(
            URI_LIST_MIME,
            b"file:///tmp/100%25.txt\nfile:///tmp/50%off.txt\n",
        );

        assert_eq!(list.paths(), ["/tmp/100%.txt", "/tmp/50%off.txt"]);
    }

    #[test]
    fn text_preview_is_one_line() {
        let text = entry(TEXT, b"  fn main() {\n    println!(\"hi\");\n}  ");

        assert_eq!(text.preview(80), "fn main() { println!(\"hi\"); }");
    }

    #[test]
    fn preview_is_cut_on_a_character_boundary() {
        let text = entry(TEXT, "é".repeat(40).as_bytes());

        let preview = text.preview(10);

        assert_eq!(preview.chars().count(), 10);
        assert!(preview.ends_with('…'));
        assert_eq!(entry(TEXT, b"short").preview(10), "short");
    }

    #[test]
    fn file_and_image_previews_say_what_they_are() {
        let one = entry(URI_LIST_MIME, b"file:///tmp/notes.txt\n");
        let two = entry(URI_LIST_MIME, b"file:///tmp/a.txt\nfile:///tmp/b.txt\n");
        let image = entry("image/png", &vec![0_u8; 2048]);

        assert_eq!(one.preview(80), "notes.txt");
        assert_eq!(two.preview(80), "a.txt, b.txt (2 files)");
        assert_eq!(image.preview(80), "Image · image/png · 2.0 KB");
        assert_eq!(
            entry("application/pdf", b"%PDF").preview(80),
            "application/pdf · 4 B"
        );
    }
}
