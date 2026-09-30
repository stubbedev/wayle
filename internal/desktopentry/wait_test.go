package desktopentry

import (
	"testing"
	"time"
)

// waitFor polls cond for up to five seconds: a detached child runs on
// its own schedule.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never held")
}
