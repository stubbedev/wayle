package clipboard

import (
	"errors"
	"log"
	"sync"

	"github.com/stubbedev/gelm/app"
)

// Clipboard is the session's clipboard history, kept current: it
// watches the regular selection, remembers what lands on it, and puts
// an entry back when asked (service.rs). The primary selection is not
// remembered, as in the Rust service.
//
// Entries, Get, Forget, and Clear are safe from any goroutine. Copy
// hops onto the loop goroutine itself, since claiming the selection
// is Wayland work.
type Clipboard struct {
	mu      sync.Mutex
	history *History

	dev    selectionDevice
	invoke func(func())

	// reads are the selection reads in flight, in the order their
	// selections arrived. Results are recorded strictly in that order,
	// so a slow owner's payload finishing after a quick one's cannot
	// jump ahead of it in the history. Loop goroutine only.
	reads []*pendingRead
}

// pendingRead is one selection read awaiting its bytes.
type pendingRead struct {
	mime string
	data []byte
	done bool
}

// selectionDevice is what the service needs from gelm's data-control
// device, narrowed so tests drive it with fakes.
type selectionDevice interface {
	// watch calls fn on every regular-selection change, nil when the
	// selection was cleared.
	watch(fn func(selectionOffer))
	// claim takes the regular selection with src.
	claim(src app.SelectionSource) error
}

// selectionOffer is one offered selection, as the service reads it.
type selectionOffer interface {
	Mimes() []string
	Own() bool
	Read(mime string, limit int64, done func([]byte, error)) error
}

// ErrNoDataControl is the Start failure on a compositor without
// either data-control protocol: the session simply has no clipboard
// history, and nothing else depends on it.
var ErrNoDataControl = errors.New("clipboard: compositor lacks ext-data-control-v1 and zwlr_data_control_manager_v1")

// Start begins watching the session's selection into history. Call it
// once at shell startup rather than when the launcher first opens, so
// the history covers the session instead of starting empty at the
// moment somebody goes looking for it. Fails with ErrNoDataControl
// when the compositor offers no data-control protocol.
func Start(a *app.Application, history *History) (*Clipboard, error) {
	dc, err := a.DataControl()
	if errors.Is(err, app.ErrDataControlUnavailable) {
		return nil, ErrNoDataControl
	}
	if err != nil {
		return nil, err
	}
	return start(dataControl{dc}, a.Invoke, history), nil
}

// start wires the service onto a device; invoke runs a function on
// the loop goroutine.
func start(dev selectionDevice, invoke func(func()), history *History) *Clipboard {
	c := &Clipboard{history: history, dev: dev, invoke: invoke}
	dev.watch(c.selectionChanged)
	return c
}

// Entries returns a snapshot of the history, most recent first.
func (c *Clipboard) Entries() []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.history.Entries()
}

// Get returns the entry with this id, false once it aged out.
func (c *Clipboard) Get(id uint64) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.history.Get(id)
}

// Forget drops one entry, reporting whether it was there.
func (c *Clipboard) Forget(id uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.history.Remove(id)
}

// Clear forgets everything.
func (c *Clipboard) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.history.Clear()
}

// Copy puts a remembered entry back on the clipboard, under the mime
// it arrived with (and every text alias, for text), and reports
// whether the entry was still in the history. The entry keeps its
// place: a restore is not a new copy.
func (c *Clipboard) Copy(id uint64) bool {
	entry, ok := c.Get(id)
	if !ok {
		return false
	}
	c.invoke(func() { c.claim(entry.Mime, entry.Bytes) })
	return true
}

// claim takes the selection with data under mime's offer set. Loop
// goroutine only.
func (c *Clipboard) claim(mime string, data []byte) {
	err := c.dev.claim(app.SelectionSource{
		Mimes: offerMimes(mime),
		// The receiver may ask under any mime offered; every alias is
		// an alias for the entry's own type, so the bytes are the same.
		Data: func(string) []byte { return data },
	})
	if err != nil {
		log.Printf("clipboard: cannot take the selection: %v", err)
	}
}

// selectionChanged reads whatever just landed on the clipboard into
// the history. Loop goroutine.
func (c *Clipboard) selectionChanged(offer selectionOffer) {
	// Our own restore announced back: reading it would put this
	// process on both ends of the pipe, and it is already in the
	// history, which is how it got here.
	if offer == nil || offer.Own() {
		return
	}
	mimes := offer.Mimes()
	if len(mimes) == 0 || IsSensitive(mimes) {
		return
	}
	mime, ok := BestMime(mimes)
	if !ok {
		return
	}
	c.mu.Lock()
	limit := int64(c.history.maxEntryBytes)
	c.mu.Unlock()
	if limit == 0 {
		return
	}

	read := &pendingRead{mime: mime}
	c.reads = append(c.reads, read)
	err := offer.Read(mime, limit, func(data []byte, err error) {
		// An oversized selection is refused whole, the same verdict the
		// history would give it; anything else is worth a line.
		if err != nil && !errors.Is(err, app.ErrTransferTooLarge) {
			log.Printf("clipboard: cannot read the selection (%s): %v", mime, err)
		}
		read.data, read.done = data, true
		c.flushReads()
	})
	if err != nil {
		log.Printf("clipboard: cannot read the selection (%s): %v", mime, err)
		read.done = true
		c.flushReads()
	}
}

// flushReads records every finished read at the head of the queue,
// stopping at the first one still in flight.
func (c *Clipboard) flushReads() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.reads) > 0 && c.reads[0].done {
		if r := c.reads[0]; r.data != nil {
			c.history.Push(r.mime, r.data)
		}
		c.reads[0] = nil
		c.reads = c.reads[1:]
	}
}

// dataControl adapts gelm's device to selectionDevice.
type dataControl struct{ dc *app.DataControl }

func (d dataControl) watch(fn func(selectionOffer)) {
	d.dc.OnSelection(func(sel app.Selection, offer *app.SelectionOffer) {
		if sel != app.SelectionClipboard {
			return
		}
		if offer == nil {
			fn(nil)
			return
		}
		fn(offer)
	})
}

func (d dataControl) claim(src app.SelectionSource) error {
	_, err := d.dc.SetSelection(app.SelectionClipboard, src)
	return err
}
