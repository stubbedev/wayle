package portal

import (
	"errors"
	"os"

	"github.com/stubbedev/gelm/app"
)

// waylandClipboard is the seat's data-control device on a headless
// gelm loop: the portal maps no surface, and Hold keeps the loop
// running without one.
type waylandClipboard struct {
	a   *app.Application
	dev *app.DataControl
	// ended closes when the loop stops (the compositor went away):
	// nothing runs Invokes after that.
	ended chan struct{}
}

// startWaylandClipboard connects, binds the device and runs its loop;
// ownerChanged runs on every clipboard selection change.
func startWaylandClipboard(ownerChanged func()) (clipDevice, error) {
	sess, err := app.Connect()
	if err != nil {
		return nil, err
	}
	a := app.NewApplication(sess)
	a.Hold()
	dev, err := a.DataControl()
	if err != nil {
		return nil, err
	}
	dev.OnSelection(func(sel app.Selection, _ *app.SelectionOffer) {
		if sel == app.SelectionClipboard {
			go ownerChanged()
		}
	})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		if err := a.Run(); err != nil && !errors.Is(err, app.ErrClosed) {
			warnf("clipboard: wayland loop ended: %v", err)
		}
	}()
	return &waylandClipboard{a: a, dev: dev, ended: ended}, nil
}

// onLoop runs fn on the loop goroutine and waits for it.
func (w *waylandClipboard) onLoop(fn func() error) error {
	done := make(chan error, 1)
	w.a.Invoke(func() { done <- fn() })
	select {
	case err := <-done:
		return err
	case <-w.ended:
		return errors.New("the wayland connection is gone")
	}
}

func (w *waylandClipboard) own(mimes []string, pass func(string, *os.File)) error {
	return w.onLoop(func() error {
		_, err := w.dev.SetSelection(app.SelectionClipboard, app.SelectionSource{Mimes: mimes, Pass: pass})
		return err
	})
}

func (w *waylandClipboard) read(mime string) (*os.File, error) {
	var f *os.File
	err := w.onLoop(func() error {
		offer := w.dev.Offer(app.SelectionClipboard)
		if offer == nil {
			return errors.New("no selection")
		}
		var err error
		f, err = offer.Receive(mime)
		return err
	})
	return f, err
}
