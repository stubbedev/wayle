package bar

import (
	"testing"
	"time"

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
