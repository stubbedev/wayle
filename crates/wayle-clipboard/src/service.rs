//! The running clipboard: watches the selection, remembers it, and puts an
//! entry back when asked.
//!
//! [`manager`](crate::manager) is the protocol and [`history`](crate::history)
//! is the bookkeeping; this is the loop that joins them.

use std::{
    io::{Read, Write},
    sync::{Arc, Mutex},
};

use tracing::{debug, warn};

use crate::{
    history::{Entry, History, Kind, TEXT_MIMES, best_mime, is_sensitive},
    manager::{ClipEvent, ClipboardHandle},
};

/// The most that is read out of one selection.
///
/// A clipboard offer is a pipe with no length up front, so without a cap a
/// misbehaving owner can stream until the shell is out of memory. Matches the
/// history's own entry cap, plus one byte so an entry exactly at the cap is
/// still distinguishable from one over it.
const READ_CAP: usize = crate::history::MAX_ENTRY_BYTES + 1;

/// The session's clipboard history, kept current.
#[derive(Clone)]
pub struct Clipboard {
    handle: Arc<ClipboardHandle>,
    history: Arc<Mutex<History>>,
    /// The entry wayle is currently the selection owner for, and therefore
    /// has to serve on demand.
    serving: Arc<Mutex<Option<Entry>>>,
}

impl Clipboard {
    /// Starts watching the Wayland selection.
    ///
    /// # Errors
    ///
    /// Returns an error when Wayland is unreachable or the compositor does not
    /// implement `zwlr_data_control_manager_v1`.
    pub fn start(history: History) -> Result<Self, String> {
        let (handle, mut events) = crate::manager::spawn()?;
        let clipboard = Self {
            handle: Arc::new(handle),
            history: Arc::new(Mutex::new(history)),
            serving: Arc::new(Mutex::new(None)),
        };

        let worker = clipboard.clone();
        tokio::spawn(async move {
            while let Some(event) = events.recv().await {
                match event {
                    ClipEvent::OwnerChanged => worker.record_selection().await,
                    ClipEvent::Transfer { mime, serial } => worker.serve(&mime, serial),
                }
            }
            debug!("clipboard watcher ended");
        });

        Ok(clipboard)
    }

    /// A snapshot of the history, most recent first.
    #[must_use]
    pub fn entries(&self) -> Vec<Entry> {
        self.history
            .lock()
            .map(|history| history.entries().cloned().collect())
            .unwrap_or_default()
    }

    /// The entry with this id, if it has not aged out.
    #[must_use]
    pub fn get(&self, id: u64) -> Option<Entry> {
        self.history
            .lock()
            .ok()
            .and_then(|history| history.get(id).cloned())
    }

    /// Forgets one entry.
    pub fn forget(&self, id: u64) -> bool {
        self.history
            .lock()
            .map(|mut history| history.remove(id))
            .unwrap_or(false)
    }

    /// Forgets everything.
    pub fn clear(&self) {
        if let Ok(mut history) = self.history.lock() {
            history.clear();
        }
    }

    /// Puts a remembered entry back on the clipboard.
    ///
    /// Returns whether the entry was still in the history.
    pub fn copy(&self, id: u64) -> bool {
        let Some(entry) = self.get(id) else {
            return false;
        };

        let mimes = offer_mimes(&entry);
        if let Ok(mut serving) = self.serving.lock() {
            *serving = Some(entry);
        }
        self.handle.set_selection(&mimes);
        true
    }

    /// Reads whatever just landed on the clipboard into the history.
    async fn record_selection(&self) {
        // Wayle is the owner: the offer we would read is our own, and the
        // process would be on both ends of the pipe. It is also already the
        // front of the history, which is how it got here.
        if self.serving.lock().is_ok_and(|serving| serving.is_some()) {
            return;
        }

        let mimes = self.handle.mimes();
        if mimes.is_empty() {
            return;
        }
        if is_sensitive(&mimes) {
            debug!("selection marked sensitive by its owner; not remembering it");
            return;
        }
        let Some(mime) = best_mime(&mimes) else {
            return;
        };
        let Some(fd) = self.handle.read(&mime) else {
            return;
        };

        // Off the runtime: the owner writes at its own pace, and a slow one
        // would otherwise stall every other task on this thread.
        let read = tokio::task::spawn_blocking(move || {
            let mut file = std::fs::File::from(fd);
            read_capped(&mut file, READ_CAP)
        })
        .await;

        let bytes = match read {
            Ok(Ok(bytes)) => bytes,
            Ok(Err(error)) => {
                warn!(%error, %mime, "cannot read the clipboard selection");
                return;
            }
            Err(error) => {
                warn!(%error, "the clipboard read task failed");
                return;
            }
        };

        if let Ok(mut history) = self.history.lock() {
            history.push(&mime, &bytes);
        }
    }

    /// Writes the entry wayle owns into the fd the compositor handed over.
    fn serve(&self, mime: &str, serial: u32) {
        let Some(fd) = self.handle.take_transfer_fd(serial) else {
            return;
        };
        let Some(entry) = self.serving.lock().ok().and_then(|serving| serving.clone()) else {
            return;
        };

        // The requester may ask under any mime we offered; the bytes are the
        // same for all of them, because every alias we offer is an alias for
        // this entry's own type.
        debug!(%mime, "serving a clipboard entry");
        let mut file = std::fs::File::from(fd);
        if let Err(error) = file.write_all(&entry.bytes) {
            warn!(%error, "cannot write the clipboard entry to its requester");
        }
    }
}

/// The session's clipboard, once [`start_global`] has run.
///
/// ponytail: a global, because there is exactly one Wayland selection per
/// session and the alternative is threading a handle through the launcher's
/// mode factory and every caller above it. If a second clipboard ever makes
/// sense — a second seat, a test double — this becomes a field on the shell's
/// service struct and the mode takes it as an argument.
static GLOBAL: std::sync::OnceLock<Clipboard> = std::sync::OnceLock::new();

/// Starts the session clipboard, if it has not started already.
///
/// Called once at shell startup rather than when the launcher first opens, so
/// the history covers the session rather than starting empty at the moment
/// somebody goes looking for it.
///
/// # Errors
///
/// Returns an error when Wayland is unreachable or the compositor does not
/// implement `zwlr_data_control_manager_v1`.
pub fn start_global(history: History) -> Result<Clipboard, String> {
    if let Some(existing) = GLOBAL.get() {
        return Ok(existing.clone());
    }
    let clipboard = Clipboard::start(history)?;
    Ok(GLOBAL.get_or_init(|| clipboard).clone())
}

/// The session clipboard, or `None` where it never started.
#[must_use]
pub fn global() -> Option<Clipboard> {
    GLOBAL.get().cloned()
}

/// The mime types an entry is offered back under.
///
/// Text goes out under every flavour of text a receiver might ask for, since
/// they are all the same bytes and an application that only knows `STRING`
/// would otherwise see an empty clipboard. Everything else is offered under
/// exactly what it came in as: a PNG is not also a `text/plain`.
#[must_use]
pub fn offer_mimes(entry: &Entry) -> Vec<String> {
    if entry.kind() != Kind::Text {
        return vec![entry.mime.clone()];
    }

    let mut mimes = vec![entry.mime.clone()];
    mimes.extend(
        TEXT_MIMES
            .iter()
            .filter(|mime| **mime != entry.mime)
            .map(|mime| (*mime).to_owned()),
    );
    mimes
}

/// Reads to end of input, refusing to grow past `cap`.
///
/// The cap is a limit on what is *kept*, not a truncation: a selection larger
/// than the cap yields the oversized buffer, which the history then refuses
/// whole rather than storing half a payload.
fn read_capped(source: &mut impl Read, cap: usize) -> std::io::Result<Vec<u8>> {
    let mut buffer = Vec::new();
    let mut chunk = [0_u8; 8192];

    loop {
        let read = source.read(&mut chunk)?;
        if read == 0 {
            return Ok(buffer);
        }
        buffer.extend_from_slice(&chunk[..read]);
        if buffer.len() >= cap {
            return Ok(buffer);
        }
    }
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

    #[test]
    fn text_is_offered_under_every_text_flavour_once() {
        let mimes = offer_mimes(&entry("text/plain;charset=utf-8", b"hi"));

        assert_eq!(
            mimes.first().map(String::as_str),
            Some("text/plain;charset=utf-8")
        );
        assert!(mimes.iter().any(|mime| mime == "STRING"));
        // The entry's own mime is in TEXT_MIMES too, and must not be offered
        // twice — a duplicate offer is a protocol error for some compositors.
        assert_eq!(
            mimes
                .iter()
                .filter(|mime| *mime == "text/plain;charset=utf-8")
                .count(),
            1
        );
        assert_eq!(mimes.len(), TEXT_MIMES.len());
    }

    #[test]
    fn non_text_is_offered_only_as_itself() {
        assert_eq!(offer_mimes(&entry("image/png", b"\x89PNG")), ["image/png"]);
        assert_eq!(
            offer_mimes(&entry("text/uri-list", b"file:///tmp/a")),
            ["text/uri-list"]
        );
    }

    #[test]
    fn a_text_mime_outside_the_known_list_still_leads_its_own_offer() {
        let mimes = offer_mimes(&entry("text/html", b"<p>"));

        assert_eq!(mimes.first().map(String::as_str), Some("text/html"));
        assert_eq!(mimes.len(), TEXT_MIMES.len() + 1);
    }

    #[test]
    fn reading_stops_at_end_of_input() {
        let mut source = std::io::Cursor::new(b"hello".to_vec());

        assert_eq!(read_capped(&mut source, 1024).unwrap(), b"hello");
    }

    #[test]
    fn reading_stops_once_past_the_cap() {
        let mut source = std::io::Cursor::new(vec![b'x'; 100_000]);

        let read = read_capped(&mut source, 16).unwrap();

        // At least the cap, and nowhere near the whole stream: the point is
        // that a huge selection cannot be read into memory in full.
        assert!(read.len() >= 16);
        assert!(read.len() < 100_000);
    }

    #[test]
    fn an_empty_selection_reads_as_nothing() {
        let mut source = std::io::Cursor::new(Vec::new());

        assert!(read_capped(&mut source, 1024).unwrap().is_empty());
    }

    #[test]
    fn a_read_error_is_reported_rather_than_silently_truncating() {
        struct Broken;
        impl Read for Broken {
            fn read(&mut self, _: &mut [u8]) -> std::io::Result<usize> {
                Err(std::io::Error::other("boom"))
            }
        }

        assert!(read_capped(&mut Broken, 1024).is_err());
    }

    #[test]
    fn an_oversized_selection_is_refused_by_the_history_not_stored_in_half() {
        let mut history = History::new(4, 8);
        let mut source = std::io::Cursor::new(vec![b'x'; 64]);
        let read = read_capped(&mut source, 9).unwrap();

        // read_capped hands back more than the history will take, and the
        // history refuses it whole rather than keeping a prefix.
        assert!(read.len() > 8);
        assert!(history.push("text/plain", &read).is_none());
        assert!(history.is_empty());
    }
}
