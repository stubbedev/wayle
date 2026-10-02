package pipewire

import (
	"errors"
	"runtime"
)

// loopThread runs one pw_loop on a goroutine locked to its OS thread,
// and every PipeWire call of its streams happens there. Stream events
// therefore arrive on a Go thread, inside iterate: PipeWire's own
// pw_thread_loop calls them from a C thread, which purego's cgo-less
// callbacks do not survive reliably (its stop could hang under load).
type loopThread struct {
	lib  *libpw
	loop *cPwLoop
	work chan func()
	done chan struct{}
}

// iterateMs is how long an idle iterate waits before taking queued
// work (node ids, teardown): rare requests, so a short poll.
const iterateMs = 20

func startLoop(l *libpw) (*loopThread, error) {
	t := &loopThread{lib: l, work: make(chan func()), done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(t.done)
		t.loop = l.loopNew(nil)
		if t.loop == nil {
			ready <- errors.New("pipewire: cannot create a loop")
			return
		}
		t.loop.enter()
		ready <- nil
		for {
			select {
			case fn, ok := <-t.work:
				if !ok {
					t.loop.leave()
					l.loopDestroy(t.loop)
					return
				}
				fn()
			default:
				t.loop.iterate(iterateMs)
			}
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

// do runs fn on the loop thread and waits for it.
func (t *loopThread) do(fn func()) {
	finished := make(chan struct{})
	t.work <- func() {
		defer close(finished)
		fn()
	}
	<-finished
}

// stop ends the loop once its queued work ran.
func (t *loopThread) stop() {
	close(t.work)
	<-t.done
}
