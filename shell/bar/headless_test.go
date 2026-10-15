package bar

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// onHeadlessLoop reads widget state the way the loop would: under
// headlessLoop, which every headless Invoke holds while it mutates.
func onHeadlessLoop[T any](read func() T) T {
	headlessLoop.Lock()
	defer headlessLoop.Unlock()
	return read()
}

// waitHeadless polls cond on the headless loop until it holds or the
// deadline passes (followers deliver from their own goroutines).
func waitHeadless(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !onHeadlessLoop(cond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitForText waits until the label carries want.
func waitForText(t *testing.T, label *widget.Label, want string) {
	t.Helper()
	waitHeadless(t, "label "+want, func() bool { return label.Text() == want })
}

// Work waiting for the loop when its generation retires is dropped:
// the retirement is read under the loop lock, as the application loop
// reads it inside the loop.
func TestHeadlessInvokeDropsWorkRetiredWhileQueued(t *testing.T) {
	ctx := ModuleContext{gen: newMountGen()}
	var ran atomic.Bool
	done := make(chan struct{})
	headlessLoop.Lock()
	go func() {
		ctx.Invoke(func() { ran.Store(true) })
		close(done)
	}()
	time.Sleep(20 * time.Millisecond) // let the call queue on the lock
	ctx.gen.retire()
	headlessLoop.Unlock()
	<-done
	if ran.Load() {
		t.Fatal("work queued before the retirement ran after it")
	}
}

// Headless Invoke runs the work inline and serialized; a retired
// generation's work is dropped.
func TestHeadlessInvokeRunsInlineUntilRetired(t *testing.T) {
	ctx := ModuleContext{gen: newMountGen()}
	ran := false
	ctx.Invoke(func() { ran = true })
	if !ran {
		t.Fatal("headless Invoke did not run the work")
	}
	ctx.gen.retire()
	ran = false
	ctx.Invoke(func() { ran = true })
	if ran {
		t.Fatal("headless Invoke ran work for a retired generation")
	}
}

// findByClass walks a tree for the first widget carrying class.
func findByClass(w widget.Widget, class string) widget.Widget {
	if c, ok := w.(interface{ HasClass(string) bool }); ok && c.HasClass(class) {
		return w
	}
	if box, ok := w.(interface{ Children() []widget.Widget }); ok {
		for _, kid := range box.Children() {
			if hit := findByClass(kid, class); hit != nil {
				return hit
			}
		}
	}
	return nil
}

// routeClick presses and releases at the center of w through a router
// over root, which must already be arranged.
func routeClick(root widget.Widget, w widget.Widget) {
	b := w.(interface{ Bounds() render.Rect }).Bounds()
	p := widget.Point{X: b.X + b.W/2, Y: b.Y + b.H/2}
	r := &widget.Router{Root: root}
	r.Move(p)
	r.Press(widget.BTNLeft, p)
	r.Release(widget.BTNLeft, p)
}
