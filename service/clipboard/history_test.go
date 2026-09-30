package clipboard

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

const textMime = "text/plain;charset=utf-8"

func entry(mime, data string) Entry { return Entry{ID: 1, Mime: mime, Bytes: []byte(data)} }

func mustHistory(t *testing.T, capacity, maxBytes int) *History {
	t.Helper()
	h, err := NewHistory(capacity, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func texts(h *History) []string {
	var out []string
	for _, e := range h.Entries() {
		if s, ok := e.Text(); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestNewestSelectionComesFirst(t *testing.T) {
	h := DefaultHistory()
	if _, ok := h.Push(textMime, []byte("one")); !ok {
		t.Fatal("refused")
	}
	h.Push(textMime, []byte("two"))

	if got := texts(h); !slices.Equal(got, []string{"two", "one"}) {
		t.Fatalf("entries = %v", got)
	}
}

func TestRecopyingMovesTheEntryWithoutDuplicatingIt(t *testing.T) {
	h := DefaultHistory()
	first, _ := h.Push(textMime, []byte("one"))
	h.Push(textMime, []byte("two"))
	h.Push(textMime, []byte("three"))

	again, ok := h.Push(textMime, []byte("one"))

	if !ok || again != first {
		t.Fatalf("recopy id = %d, want %d", again, first)
	}
	if got := texts(h); !slices.Equal(got, []string{"one", "three", "two"}) {
		t.Fatalf("entries = %v", got)
	}
}

func TestTheSameBytesUnderADifferentMimeIsADifferentEntry(t *testing.T) {
	h := DefaultHistory()
	h.Push(textMime, []byte("data"))
	h.Push("image/png", []byte("data"))

	if h.Len() != 2 {
		t.Fatalf("len = %d, want 2", h.Len())
	}
}

func TestTheOldestEntryFallsOffTheEnd(t *testing.T) {
	h := mustHistory(t, 2, MaxEntryBytes)
	h.Push(textMime, []byte("one"))
	h.Push(textMime, []byte("two"))
	h.Push(textMime, []byte("three"))

	if got := texts(h); !slices.Equal(got, []string{"three", "two"}) {
		t.Fatalf("entries = %v", got)
	}
}

func TestSelectionsNotWorthRememberingAreRefused(t *testing.T) {
	h := mustHistory(t, 4, 8)

	for _, data := range []string{"", "   \n\t ", "123456789"} {
		if _, ok := h.Push(textMime, []byte(data)); ok {
			t.Errorf("%q was remembered", data)
		}
	}
	if h.Len() != 0 {
		t.Fatal("a refused selection was stored")
	}
	// Exactly at the cap is fine, and whitespace-looking binary is
	// content rather than a stray drag.
	if _, ok := h.Push(textMime, []byte("12345678")); !ok {
		t.Error("a selection exactly at the cap was refused")
	}
	if _, ok := h.Push("image/png", []byte("  \n  ")); !ok {
		t.Error("whitespace-looking binary was refused")
	}
	if h.Len() != 2 {
		t.Fatalf("len = %d, want 2", h.Len())
	}
}

func TestAZeroCapacityHistoryRemembersNothing(t *testing.T) {
	h := mustHistory(t, 0, MaxEntryBytes)

	if _, ok := h.Push(textMime, []byte("one")); ok || h.Len() != 0 {
		t.Fatal("a zero-capacity history remembered something")
	}
}

func TestNegativeBoundsAreAnError(t *testing.T) {
	if _, err := NewHistory(-1, 8); err == nil {
		t.Error("negative capacity accepted")
	}
	if _, err := NewHistory(8, -1); err == nil {
		t.Error("negative entry size accepted")
	}
}

func TestAnAgedOutOrRemovedIDResolvesToNothing(t *testing.T) {
	h := mustHistory(t, 1, MaxEntryBytes)
	old, _ := h.Push(textMime, []byte("one"))
	current, _ := h.Push(textMime, []byte("two"))

	if _, ok := h.Get(old); ok {
		t.Error("an aged-out id resolved")
	}
	if _, ok := h.Get(^uint64(0)); ok {
		t.Error("an unknown id resolved")
	}
	if e, ok := h.Get(current); !ok || string(e.Bytes) != "two" {
		t.Errorf("current id = %v, %v", e, ok)
	}
	if !h.Remove(current) || h.Remove(current) {
		t.Error("Remove did not report presence exactly once")
	}
	if h.Len() != 0 {
		t.Fatal("history not empty")
	}
}

func TestClearForgetsEverything(t *testing.T) {
	h := DefaultHistory()
	h.Push(textMime, []byte("one"))
	h.Push(textMime, []byte("two"))

	h.Clear()

	if h.Len() != 0 || len(h.Entries()) != 0 {
		t.Fatal("Clear left entries")
	}
	// Ids keep counting: a row drawn before the clear cannot name a
	// selection made after it.
	id, _ := h.Push(textMime, []byte("three"))
	if id != 3 {
		t.Fatalf("id after clear = %d, want 3", id)
	}
}

func TestEntriesIsASnapshot(t *testing.T) {
	h := DefaultHistory()
	h.Push(textMime, []byte("one"))
	snap := h.Entries()

	h.Push(textMime, []byte("two"))

	if len(snap) != 1 {
		t.Fatal("a snapshot changed under a later push")
	}
}

func TestAPasswordManagerHintKeepsTheSecretOut(t *testing.T) {
	if !IsSensitive([]string{textMime, "x-kde-passwordManagerHint"}) {
		t.Error("the KDE hint was not honoured")
	}
	// The ordinary case must not be swept up with it.
	if IsSensitive([]string{textMime, "text/html"}) || IsSensitive(nil) {
		t.Error("an ordinary selection read as sensitive")
	}
}

func best(t *testing.T, mimes ...string) string {
	t.Helper()
	m, ok := BestMime(mimes)
	if !ok {
		t.Fatalf("BestMime(%v) found nothing", mimes)
	}
	return m
}

func TestFilesBeatTheFilenameTextOfferedAlongsideThem(t *testing.T) {
	// What a file manager actually offers when you copy one file.
	if got := best(t, textMime, "text/plain", URIListMime, "text/html"); got != URIListMime {
		t.Fatalf("best = %q", got)
	}
}

func TestAnImageBeatsTextButFilesBeatAnImage(t *testing.T) {
	if got := best(t, "text/plain", "image/png"); got != "image/png" {
		t.Errorf("best = %q", got)
	}
	if got := best(t, "image/png", URIListMime); got != URIListMime {
		t.Errorf("best = %q", got)
	}
}

func TestUTF8TextIsPreferredOverTheOlderFlavours(t *testing.T) {
	if got := best(t, "STRING", "TEXT", textMime); got != textMime {
		t.Errorf("best = %q", got)
	}
	if got := best(t, "STRING", "TEXT"); got != "STRING" {
		t.Errorf("best = %q", got)
	}
	if got := best(t, "application/x-thing", "text/html"); got != "text/html" {
		t.Errorf("an unlisted text/ mime lost to a non-text one: %q", got)
	}
}

func TestAnUnknownSelectionIsStillKeptAndNothingOfferedIsNone(t *testing.T) {
	if got := best(t, "application/x-thing"); got != "application/x-thing" {
		t.Errorf("best = %q", got)
	}
	if _, ok := BestMime(nil); ok {
		t.Error("nothing offered still picked a mime")
	}
}

func TestKindsComeFromTheMime(t *testing.T) {
	cases := map[Kind]Entry{
		KindText:  entry(textMime, "hi"),
		KindFiles: entry(URIListMime, "file:///a"),
		KindImage: entry("image/png", "\x89PNG"),
		KindOther: entry("application/pdf", "%PDF"),
	}
	for want, e := range cases {
		if got := e.Kind(); got != want {
			t.Errorf("%s: kind = %v, want %v", e.Mime, got, want)
		}
	}
	if got := entry("text/html", "<p>").Kind(); got != KindText {
		t.Errorf("text/html kind = %v", got)
	}
}

func TestTextOnlyForTextAndValidUTF8(t *testing.T) {
	if s, ok := entry(textMime, "hi").Text(); !ok || s != "hi" {
		t.Errorf("text = %q, %v", s, ok)
	}
	if _, ok := entry("image/png", "hi").Text(); ok {
		t.Error("an image read as text")
	}
	if _, ok := entry(textMime, "\xff\xfe").Text(); ok {
		t.Error("invalid UTF-8 read as text")
	}
}

func TestURIListYieldsDecodedPaths(t *testing.T) {
	list := entry(URIListMime, "# a comment\r\nfile:///home/me/my%20photo.png\r\nfile:///tmp/notes.txt\r\n")

	if got := list.Paths(); !slices.Equal(got, []string{"/home/me/my photo.png", "/tmp/notes.txt"}) {
		t.Fatalf("paths = %v", got)
	}
}

func TestANonFileURIAndANonListEntryYieldNoPaths(t *testing.T) {
	// Only file:// URIs name something on disk.
	if got := entry(URIListMime, "https://example.com/a\n").Paths(); len(got) != 0 {
		t.Errorf("paths = %v", got)
	}
	// Text that happens to look like one is still text.
	if got := entry(textMime, "file:///tmp/a").Paths(); len(got) != 0 {
		t.Errorf("paths = %v", got)
	}
}

func TestAStrayPercentInANameSurvivesDecoding(t *testing.T) {
	list := entry(URIListMime, "file:///tmp/100%25.txt\nfile:///tmp/50%off.txt\nfile:///tmp/end%\nfile:///tmp/end%a\n")

	want := []string{"/tmp/100%.txt", "/tmp/50%off.txt", "/tmp/end%", "/tmp/end%a"}
	if got := list.Paths(); !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestTextPreviewIsOneLine(t *testing.T) {
	text := entry(textMime, "  fn main() {\n    println!(\"hi\");\n}  ")

	if got := text.Preview(80); got != `fn main() { println!("hi"); }` {
		t.Fatalf("preview = %q", got)
	}
}

func TestPreviewIsCutOnACharacterBoundary(t *testing.T) {
	text := entry(textMime, strings.Repeat("é", 40))

	got := text.Preview(10)

	if utf8.RuneCountInString(got) != 10 || !strings.HasSuffix(got, "…") {
		t.Fatalf("preview = %q", got)
	}
	if got := entry(textMime, "short").Preview(10); got != "short" {
		t.Fatalf("a short preview was cut: %q", got)
	}
}

func TestFileAndImagePreviewsSayWhatTheyAre(t *testing.T) {
	cases := map[string]Entry{
		"notes.txt":                  entry(URIListMime, "file:///tmp/notes.txt\n"),
		"a.txt, b.txt (2 files)":     entry(URIListMime, "file:///tmp/a.txt\nfile:///tmp/b.txt\n"),
		"No files":                   entry(URIListMime, "https://example.com\n"),
		"Image · image/png · 2.0 KB": {Mime: "image/png", Bytes: make([]byte, 2048)},
		"application/pdf · 4 B":      entry("application/pdf", "%PDF"),
		"Image · image/png · 1.5 MB": {Mime: "image/png", Bytes: make([]byte, 3<<19)},
	}
	for want, e := range cases {
		if got := e.Preview(80); got != want {
			t.Errorf("preview = %q, want %q", got, want)
		}
	}
}

func TestTextIsOfferedUnderEveryTextFlavourOnce(t *testing.T) {
	mimes := offerMimes(textMime)

	if mimes[0] != textMime || !slices.Contains(mimes, "STRING") {
		t.Fatalf("mimes = %v", mimes)
	}
	// The entry's own mime is in TextMimes too, and must not be
	// offered twice — a duplicate offer is a protocol error for some
	// compositors.
	if len(mimes) != len(TextMimes) {
		t.Fatalf("mimes = %v, want each text flavour once", mimes)
	}
}

func TestNonTextIsOfferedOnlyAsItself(t *testing.T) {
	if got := offerMimes("image/png"); !slices.Equal(got, []string{"image/png"}) {
		t.Errorf("png offered as %v", got)
	}
	if got := offerMimes(URIListMime); !slices.Equal(got, []string{URIListMime}) {
		t.Errorf("uri-list offered as %v", got)
	}
}

func TestATextMimeOutsideTheKnownListStillLeadsItsOwnOffer(t *testing.T) {
	mimes := offerMimes("text/html")

	if mimes[0] != "text/html" || len(mimes) != len(TextMimes)+1 {
		t.Fatalf("mimes = %v", mimes)
	}
}
