package portal

import (
	"errors"
	"sync"

	"github.com/stubbedev/gelm/app"
)

// waylandLoop is the portal's one Wayland connection: a headless gelm
// loop, started on first use by the interfaces that need the
// compositor (Clipboard's data-control device, GlobalShortcuts). It
// maps no surface; Hold keeps it running without one.
type waylandLoop struct {
	mu sync.Mutex
	a  *app.Application
	// ended closes when the loop stops (the compositor went away):
	// nothing runs queued work after that.
	ended chan struct{}
}

// do runs fn on the loop goroutine, starting the loop first, and waits
// for it.
func (w *waylandLoop) do(fn func(*app.Application) error) error {
	a, ended, err := w.start()
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	a.Invoke(func() { done <- fn(a) })
	select {
	case err := <-done:
		return err
	case <-ended:
		return errors.New("the wayland connection is gone")
	}
}

func (w *waylandLoop) start() (*app.Application, chan struct{}, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.a != nil {
		return w.a, w.ended, nil
	}
	sess, err := app.Connect()
	if err != nil {
		return nil, nil, err
	}
	a := app.NewApplication(sess)
	a.Hold()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		if err := a.Run(); err != nil && !errors.Is(err, app.ErrClosed) {
			warnf("wayland loop ended: %v", err)
		}
	}()
	w.a, w.ended = a, ended
	return a, ended, nil
}
