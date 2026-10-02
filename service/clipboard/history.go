// Package clipboard is the session's clipboard history
// (crates/wayle-clipboard): what gets remembered, how it is labelled,
// what is refused, and the service that keeps it current from the
// Wayland selection over gelm's data-control device.
//
// An entry is bytes plus the mime type they arrived under, not a
// string. A file manager puts text/uri-list on the clipboard, an
// image editor image/png, a terminal text/plain;charset=utf-8 —
// remembering only the last of those would make the history useless
// for exactly the things that are most annoying to copy twice.
package clipboard

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/wayle/internal/fileuri"
)

// DefaultCapacity is how many entries are kept before the oldest is
// dropped (history.rs DEFAULT_CAPACITY).
const DefaultCapacity = 200

// MaxEntryBytes is the largest selection worth remembering. The
// history lives in memory for the session, and a copied image or a
// large file list is unbounded. Past this the entry is refused rather
// than truncated: half a payload pasted back is worse than not
// offering it.
const MaxEntryBytes = 8 * 1024 * 1024

// TextMimes are the mime types a plain-text selection arrives under,
// best first.
var TextMimes = []string{
	"text/plain;charset=utf-8",
	"text/plain",
	"UTF8_STRING",
	"STRING",
	"TEXT",
}

// URIListMime is the mime type a file manager copies files under.
const URIListMime = "text/uri-list"

// SensitiveMimes are the mime types wayle will not remember, however
// they are offered. Password managers mark a selection with one of
// these to ask clipboard managers not to keep it, and honouring that
// is the difference between a convenience and a credential leak.
// x-kde-passwordManagerHint carries the value "secret"; the others
// are presence-only conventions.
var SensitiveMimes = []string{
	"x-kde-passwordManagerHint",
	"org.kde.passwordManagerHint",
	"password",
	"x-wayle-no-history",
}

// Kind is what a remembered selection is, for labelling and for
// choosing an icon.
type Kind uint8

const (
	// KindText is plain text.
	KindText Kind = iota
	// KindFiles is one or more file:// URIs — what copying in a file
	// manager puts down.
	KindFiles
	// KindImage is an image, in whatever encoding the mime names.
	KindImage
	// KindOther is anything else, kept verbatim and offered back
	// under its own mime.
	KindOther
)

// String names the kind.
func (k Kind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindFiles:
		return "files"
	case KindImage:
		return "image"
	case KindOther:
		return "other"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Entry is one remembered selection.
type Entry struct {
	// ID is stable for the life of the entry, so a row the launcher
	// drew still names the same selection once something new arrives
	// at the front.
	ID uint64
	// Mime is the type the bytes are in, and the one they are offered
	// back under.
	Mime string
	// Bytes is the selection, exactly as it was copied.
	Bytes []byte
}

// Kind reports what the entry is, from its mime type.
func (e Entry) Kind() Kind {
	switch {
	case e.Mime == URIListMime:
		return KindFiles
	case strings.HasPrefix(e.Mime, "image/"):
		return KindImage
	case IsTextMime(e.Mime):
		return KindText
	}
	return KindOther
}

// Text returns the entry as text, when it is text (or a file list)
// and valid UTF-8.
func (e Entry) Text() (string, bool) {
	if k := e.Kind(); k != KindText && k != KindFiles {
		return "", false
	}
	if !utf8.Valid(e.Bytes) {
		return "", false
	}
	return string(e.Bytes), true
}

// Paths returns the decoded paths in a text/uri-list entry. Comment
// lines are part of the format (RFC 2483) and are not paths, and a
// URI that is not file:// has no path to give.
func (e Entry) Paths() []string {
	if e.Kind() != KindFiles || !utf8.Valid(e.Bytes) {
		return nil
	}
	var paths []string
	for line := range strings.SplitSeq(string(e.Bytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "file://"); ok {
			paths = append(paths, fileuri.DecodeString(rest))
		}
	}
	return paths
}

// Preview is a single line for a list row, at most maxChars
// characters.
func (e Entry) Preview(maxChars int) string {
	var label string
	switch e.Kind() {
	case KindText:
		// Runs of whitespace — newlines included — collapse to one
		// space, so a copied paragraph is one readable row rather
		// than something that breaks the list's geometry.
		text, _ := e.Text()
		label = strings.Join(strings.Fields(text), " ")
	case KindFiles:
		paths := e.Paths()
		names := make([]string, len(paths))
		for i, p := range paths {
			names[i] = p[strings.LastIndexByte(p, '/')+1:]
		}
		switch len(names) {
		case 0:
			label = "No files"
		case 1:
			label = names[0]
		default:
			label = fmt.Sprintf("%s (%d files)", strings.Join(names, ", "), len(names))
		}
	case KindImage:
		label = "Image · " + e.Mime + " · " + humanSize(len(e.Bytes))
	case KindOther:
		label = e.Mime + " · " + humanSize(len(e.Bytes))
	}
	return truncateChars(label, maxChars)
}

// IsTextMime reports whether a mime type names plain text.
func IsTextMime(mime string) bool {
	return slices.Contains(TextMimes, mime) || strings.HasPrefix(mime, "text/")
}

// IsSensitive reports whether a selection offering these mimes must
// not be remembered.
func IsSensitive(mimes []string) bool {
	return slices.ContainsFunc(mimes, func(m string) bool { return slices.Contains(SensitiveMimes, m) })
}

// BestMime picks the mime worth remembering out of everything the
// owner offered. Files beat images beat text: a file manager also
// offers the file's name as text/plain, and remembering that instead
// would put a bare filename on the clipboard where a file was copied.
// Within text, the order of TextMimes wins so the entry is UTF-8
// wherever the owner can provide it. Anything else is still worth
// being able to paste back, byte for byte, so the first offered mime
// is the last resort; false only when nothing was offered.
func BestMime(mimes []string) (string, bool) {
	if slices.Contains(mimes, URIListMime) {
		return URIListMime, true
	}
	if i := slices.IndexFunc(mimes, func(m string) bool { return strings.HasPrefix(m, "image/") }); i >= 0 {
		return mimes[i], true
	}
	for _, text := range TextMimes {
		if slices.Contains(mimes, text) {
			return text, true
		}
	}
	if i := slices.IndexFunc(mimes, func(m string) bool { return strings.HasPrefix(m, "text/") }); i >= 0 {
		return mimes[i], true
	}
	if len(mimes) > 0 {
		return mimes[0], true
	}
	return "", false
}

// offerMimes are the mime types an entry of this mime is offered back
// under. Text goes out under every flavour of text a receiver might
// ask for, since they are all the same bytes and an application that
// only knows STRING would otherwise see an empty clipboard; the
// entry's own mime leads and is never offered twice. Everything else
// is offered under exactly what it came in as: a PNG is not also a
// text/plain.
func offerMimes(mime string) []string {
	if (Entry{Mime: mime}).Kind() != KindText {
		return []string{mime}
	}
	mimes := []string{mime}
	for _, m := range TextMimes {
		if m != mime {
			mimes = append(mimes, m)
		}
	}
	return mimes
}

// History is the most-recent-first clipboard history, bounded in both
// directions. It is plain bookkeeping and not safe for concurrent
// use; the Clipboard service guards it.
type History struct {
	entries       []Entry
	capacity      int
	maxEntryBytes int
	nextID        uint64
}

// NewHistory builds an empty history keeping at most capacity entries
// of at most maxEntryBytes each. A zero capacity remembers nothing; a
// negative bound is an error.
func NewHistory(capacity, maxEntryBytes int) (*History, error) {
	if capacity < 0 || maxEntryBytes < 0 {
		return nil, fmt.Errorf("clipboard: history bounds must not be negative (capacity %d, max entry %d)", capacity, maxEntryBytes)
	}
	return &History{capacity: capacity, maxEntryBytes: maxEntryBytes, nextID: 1}, nil
}

// DefaultHistory is the history the shell runs with: DefaultCapacity
// entries of up to MaxEntryBytes.
func DefaultHistory() *History {
	return &History{capacity: DefaultCapacity, maxEntryBytes: MaxEntryBytes, nextID: 1}
}

// Push records a selection and returns the id it is stored under.
// Re-copying something already remembered moves it to the front and
// keeps its id rather than adding a second copy: the clipboard is a
// stack of what you have, and the same bytes twice is one thing. The
// same bytes under another mime are another entry — pasting a PNG
// back as text/plain would hand the receiver bytes it cannot use.
//
// False when the selection is not worth remembering: empty,
// oversized, whitespace-only text, or a zero-capacity history.
func (h *History) Push(mime string, data []byte) (uint64, bool) {
	if len(data) == 0 || len(data) > h.maxEntryBytes || h.capacity == 0 {
		return 0, false
	}
	// Whitespace-only text comes from a stray drag as often as from
	// intent, and is unrecognisable as a row. Non-text is judged by
	// length alone: bytes that look like whitespace are still content.
	if IsTextMime(mime) && utf8.Valid(data) && strings.TrimSpace(string(data)) == "" {
		return 0, false
	}

	if i := slices.IndexFunc(h.entries, func(e Entry) bool { return e.Mime == mime && bytes.Equal(e.Bytes, data) }); i >= 0 {
		entry := h.entries[i]
		copy(h.entries[1:i+1], h.entries[:i])
		h.entries[0] = entry
		return entry.ID, true
	}

	id := h.nextID
	h.nextID++
	h.entries = slices.Insert(h.entries, 0, Entry{ID: id, Mime: mime, Bytes: slices.Clone(data)})
	if len(h.entries) > h.capacity {
		clear(h.entries[h.capacity:])
		h.entries = h.entries[:h.capacity]
	}
	return id, true
}

// Entries returns every entry, most recent first. The slice is a
// copy; the byte slices are shared and must not be modified.
func (h *History) Entries() []Entry { return slices.Clone(h.entries) }

// Get returns the entry with this id, false once it aged out or was
// removed.
func (h *History) Get(id uint64) (Entry, bool) {
	i := slices.IndexFunc(h.entries, func(e Entry) bool { return e.ID == id })
	if i < 0 {
		return Entry{}, false
	}
	return h.entries[i], true
}

// Remove forgets one entry, reporting whether it was there.
func (h *History) Remove(id uint64) bool {
	before := len(h.entries)
	h.entries = slices.DeleteFunc(h.entries, func(e Entry) bool { return e.ID == id })
	return len(h.entries) != before
}

// Clear forgets everything.
func (h *History) Clear() { h.entries = nil }

// Len reports how many entries are held.
func (h *History) Len() int { return len(h.entries) }

// truncateChars cuts to maxChars characters, not bytes: a byte-wise
// cut would split a multi-byte character.
func truncateChars(text string, maxChars int) string {
	if utf8.RuneCountInString(text) <= maxChars {
		return text
	}
	keep := max(maxChars-1, 0)
	var b strings.Builder
	for _, r := range text {
		if keep == 0 {
			break
		}
		b.WriteRune(r)
		keep--
	}
	b.WriteRune('…')
	return b.String()
}

// humanSize is a byte count short enough for a list row.
func humanSize(n int) string {
	units := [...]string{"B", "KB", "MB", "GB"}
	size := float64(n)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}
