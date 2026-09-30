package clipboard

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stubbedev/gelm/app"
)

// fakeDevice plays the data-control device: the test announces
// offers through announce, and claims are recorded.
type fakeDevice struct {
	fn       func(selectionOffer)
	claims   []app.SelectionSource
	claimErr error
}

func (d *fakeDevice) watch(fn func(selectionOffer)) { d.fn = fn }

func (d *fakeDevice) claim(src app.SelectionSource) error {
	if d.claimErr != nil {
		return d.claimErr
	}
	d.claims = append(d.claims, src)
	return nil
}

func (d *fakeDevice) announce(o selectionOffer) { d.fn(o) }

// fakeOffer holds its read open until the test finishes it.
type fakeOffer struct {
	mimes   []string
	own     bool
	readErr error
	asked   []string
	limit   int64
	done    func([]byte, error)
}

func (o *fakeOffer) Mimes() []string { return o.mimes }
func (o *fakeOffer) Own() bool       { return o.own }

func (o *fakeOffer) Read(mime string, limit int64, done func([]byte, error)) error {
	if o.readErr != nil {
		return o.readErr
	}
	o.asked = append(o.asked, mime)
	o.limit = limit
	o.done = done
	return nil
}

func (o *fakeOffer) finish(data string) { o.done([]byte(data), nil) }

// newTestClipboard runs invoke inline: the tests are the loop.
func newTestClipboard(t *testing.T, h *History) (*Clipboard, *fakeDevice) {
	t.Helper()
	if h == nil {
		h = DefaultHistory()
	}
	dev := &fakeDevice{}
	return start(dev, func(fn func()) { fn() }, h), dev
}

func mimesOf(e []Entry) []string {
	out := make([]string, len(e))
	for i, x := range e {
		out[i] = fmt.Sprintf("%s=%s", x.Mime, x.Bytes)
	}
	return out
}

func TestAForeignCopyIsReadUnderItsBestMimeAndRemembered(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	offer := &fakeOffer{mimes: []string{"text/plain", URIListMime}}

	dev.announce(offer)
	offer.finish("file:///tmp/a\n")

	if !slices.Equal(offer.asked, []string{URIListMime}) {
		t.Fatalf("read under %v", offer.asked)
	}
	if offer.limit != MaxEntryBytes {
		t.Fatalf("read limit = %d, want the entry cap", offer.limit)
	}
	if got := mimesOf(c.Entries()); !slices.Equal(got, []string{URIListMime + "=file:///tmp/a\n"}) {
		t.Fatalf("entries = %v", got)
	}
}

func TestSelectionsThatMustNotBeReadAreNot(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	cases := map[string]*fakeOffer{
		"own":       {mimes: []string{textMime}, own: true},
		"sensitive": {mimes: []string{textMime, "x-kde-passwordManagerHint"}},
		"empty":     {},
	}
	for name, offer := range cases {
		dev.announce(offer)
		if len(offer.asked) != 0 {
			t.Errorf("%s selection was read", name)
		}
	}
	dev.announce(nil)
	if len(c.Entries()) != 0 {
		t.Fatal("something was remembered")
	}
}

func TestReadsAreRecordedInSelectionOrder(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	slow := &fakeOffer{mimes: []string{textMime}}
	quick := &fakeOffer{mimes: []string{textMime}}

	dev.announce(slow)
	dev.announce(quick)
	quick.finish("second")
	if len(c.Entries()) != 0 {
		t.Fatal("a later read was recorded ahead of an earlier one in flight")
	}
	slow.finish("first")

	if got := texts(c.history); !slices.Equal(got, []string{"second", "first"}) {
		t.Fatalf("entries = %v, want newest first", got)
	}
}

func TestAFailedReadIsSkippedWithoutBlockingTheNext(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	broken := &fakeOffer{mimes: []string{textMime}}
	refused := &fakeOffer{mimes: []string{textMime}, readErr: errors.New("gone")}
	fine := &fakeOffer{mimes: []string{textMime}}

	dev.announce(broken)
	dev.announce(refused)
	dev.announce(fine)
	fine.finish("kept")
	broken.done(nil, app.ErrTransferTooLarge)

	if got := texts(c.history); !slices.Equal(got, []string{"kept"}) {
		t.Fatalf("entries = %v", got)
	}
	if len(c.reads) != 0 {
		t.Fatalf("%d reads left queued", len(c.reads))
	}
}

func TestCopyPutsAnEntryBackUnderItsOwnMime(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	png := &fakeOffer{mimes: []string{"image/png"}}
	dev.announce(png)
	png.finish("\x89PNG")
	text := &fakeOffer{mimes: []string{textMime}}
	dev.announce(text)
	text.finish("hello")
	entries := c.Entries()

	if !c.Copy(entries[1].ID) {
		t.Fatal("Copy of a remembered entry reported false")
	}

	if len(dev.claims) != 1 || !slices.Equal(dev.claims[0].Mimes, []string{"image/png"}) {
		t.Fatalf("claims = %+v", dev.claims)
	}
	if got := string(dev.claims[0].Data("image/png")); got != "\x89PNG" {
		t.Fatalf("served %q", got)
	}
	// A restore is not a new copy: the order is unchanged.
	if got := mimesOf(c.Entries()); !slices.Equal(got, mimesOf(entries)) {
		t.Fatalf("entries moved: %v", got)
	}
}

func TestCopyingTextOffersEveryTextAlias(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	offer := &fakeOffer{mimes: []string{"STRING", textMime}}
	dev.announce(offer)
	offer.finish("hi")

	c.Copy(c.Entries()[0].ID)

	if got := dev.claims[0].Mimes; got[0] != textMime || len(got) != len(TextMimes) {
		t.Fatalf("offered %v", got)
	}
	if got := string(dev.claims[0].Data("STRING")); got != "hi" {
		t.Fatalf("an alias served %q", got)
	}
}

// The Rust service stops recording for the rest of the session after
// its first restore (its serving slot is never cleared); here only the
// restore's own offer is skipped, and the next foreign copy counts.
func TestACopyAfterARestoreIsRememberedAgain(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	first := &fakeOffer{mimes: []string{textMime}}
	dev.announce(first)
	first.finish("one")
	c.Copy(c.Entries()[0].ID)
	dev.announce(&fakeOffer{mimes: []string{textMime}, own: true})

	next := &fakeOffer{mimes: []string{textMime}}
	dev.announce(next)
	next.finish("two")

	if got := texts(c.history); !slices.Equal(got, []string{"two", "one"}) {
		t.Fatalf("entries = %v", got)
	}
}

func TestCopyOfAGoneEntryClaimsNothing(t *testing.T) {
	c, dev := newTestClipboard(t, nil)

	if c.Copy(42) {
		t.Fatal("Copy of an unknown id reported true")
	}
	if len(dev.claims) != 0 {
		t.Fatal("an unknown id still claimed the selection")
	}
}

func TestForgetAndClear(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	for _, s := range []string{"a", "b", "c"} {
		o := &fakeOffer{mimes: []string{textMime}}
		dev.announce(o)
		o.finish(s)
	}
	id := c.Entries()[1].ID

	if !c.Forget(id) || c.Forget(id) {
		t.Fatal("Forget did not report presence exactly once")
	}
	if _, ok := c.Get(id); ok {
		t.Fatal("a forgotten entry still resolves")
	}
	if got := texts(c.history); !slices.Equal(got, []string{"c", "a"}) {
		t.Fatalf("entries = %v", got)
	}
	c.Clear()
	if len(c.Entries()) != 0 {
		t.Fatal("Clear left entries")
	}
}

func TestAClaimFailureIsNotFatal(t *testing.T) {
	c, dev := newTestClipboard(t, nil)
	offer := &fakeOffer{mimes: []string{textMime}}
	dev.announce(offer)
	offer.finish("x")
	dev.claimErr = errors.New("finished")

	if !c.Copy(c.Entries()[0].ID) {
		t.Fatal("Copy reported the entry gone")
	}
	if c.history.Len() != 1 {
		t.Fatal("the history lost an entry to a claim failure")
	}
}

func TestAZeroSizeHistoryReadsNothing(t *testing.T) {
	h, err := NewHistory(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, dev := newTestClipboard(t, h)
	offer := &fakeOffer{mimes: []string{textMime}}

	dev.announce(offer)

	if len(offer.asked) != 0 {
		t.Fatal("a history that keeps nothing still read the selection")
	}
}
