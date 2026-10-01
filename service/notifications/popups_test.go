package notifications

import (
	"testing"
	"time"
)

// waitPopups polls until the popup list has want entries or the
// deadline passes, returning the final count.
func waitPopups(s *Service, want int, within time.Duration) int {
	deadline := time.Now().Add(within)
	for len(s.Popups()) != want && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	return len(s.Popups())
}

func TestPopupTimesOutButStaysInHistory(t *testing.T) {
	s := newTestService(t)
	s.SetPopupDuration(30 * time.Millisecond)
	feed, _ := s.Subscribe()
	s.Notify("app", 0, "", "one", "", nil, -1)
	if got := waitPopups(s, 0, time.Second); got != 0 {
		t.Fatalf("popups after the duration = %d, want 0", got)
	}
	if got := s.Count(); got != 1 {
		t.Fatalf("history = %d, want the notification kept", got)
	}
	sawPopups := false
	for len(feed) > 0 {
		if ev := <-feed; ev.Kind == EventPopups {
			sawPopups = true
		}
	}
	if !sawPopups {
		t.Error("the countdown ran out without a popups event")
	}
}

func TestPopupNewestFirstAndReplacementMovesUp(t *testing.T) {
	s := newTestService(t)
	first := s.Notify("app", 0, "", "one", "", nil, 0)
	second := s.Notify("app", 0, "", "two", "", nil, 0)
	if got := s.Popups(); len(got) != 2 || got[0].ID != second || got[1].ID != first {
		t.Fatalf("popups = %v, want newest first", got)
	}
	s.Notify("app", first, "", "one again", "", nil, 0)
	if got := s.Popups(); len(got) != 2 || got[0].ID != first || got[0].Summary != "one again" {
		t.Fatalf("popups = %v, want the replacement on top", got)
	}
}

func TestPopupDurationRules(t *testing.T) {
	s := newTestService(t)
	s.SetPopupDuration(time.Hour)
	// A positive expire_timeout shorter than the duration caps it.
	s.Notify("app", 0, "", "short", "", nil, 20)
	// Zero never times out.
	sticky := s.Notify("app", 0, "", "sticky", "", nil, 0)
	if got := waitPopups(s, 1, time.Second); got != 1 {
		t.Fatalf("popups = %d, want only the sticky one left", got)
	}
	if s.Popups()[0].ID != sticky {
		t.Fatal("the zero-timeout popup timed out")
	}
	s.mu.Lock()
	_, hasTimer := s.timers[sticky]
	s.mu.Unlock()
	if hasTimer {
		t.Error("a zero-timeout popup got a countdown")
	}
}

func TestHoverPauseHoldsThePopup(t *testing.T) {
	s := newTestService(t)
	s.SetPopupDuration(40 * time.Millisecond)
	id := s.Notify("app", 0, "", "hover me", "", nil, -1)
	s.InhibitPopup(id)
	time.Sleep(80 * time.Millisecond)
	if got := len(s.Popups()); got != 1 {
		t.Fatalf("paused popup timed out: popups = %d", got)
	}
	s.mu.Lock()
	left := s.timers[id].duration
	s.mu.Unlock()
	if left <= 0 || left > 40*time.Millisecond {
		t.Fatalf("paused remaining = %v, want what was left of 40ms", left)
	}
	// Releasing resumes with the time left, then the popup goes.
	s.ReleasePopup(id)
	if got := waitPopups(s, 0, time.Second); got != 0 {
		t.Fatalf("released popup never timed out: popups = %d", got)
	}
}

func TestReleaseWithNoTimeLeftDropsAtOnce(t *testing.T) {
	s := newTestService(t)
	s.SetPopupDuration(time.Hour)
	id := s.Notify("app", 0, "", "gone", "", nil, -1)
	s.InhibitPopup(id)
	s.mu.Lock()
	s.timers[id].duration = 0
	s.mu.Unlock()
	s.ReleasePopup(id)
	if got := len(s.Popups()); got != 0 {
		t.Fatalf("popups = %d, want the spent popup dropped on release", got)
	}
}

func TestReleaseOnARunningCountdownIsIgnored(t *testing.T) {
	s := newTestService(t)
	s.SetPopupDuration(time.Hour)
	id := s.Notify("app", 0, "", "running", "", nil, -1)
	s.mu.Lock()
	before := s.timers[id]
	s.mu.Unlock()
	// A leave without an enter (or a double leave) must not restart the
	// countdown.
	s.ReleasePopup(id)
	s.mu.Lock()
	after := s.timers[id]
	s.mu.Unlock()
	if before != after {
		t.Error("release restarted a running countdown")
	}
	// Unknown ids are ignored outright.
	s.InhibitPopup(9999)
	s.ReleasePopup(9999)
}

func TestDismissPopupKeepsHistory(t *testing.T) {
	s := newTestService(t)
	id := s.Notify("app", 0, "", "one", "", nil, 0)
	s.DismissPopup(id)
	if got := len(s.Popups()); got != 0 {
		t.Fatalf("popups = %d after dismiss", got)
	}
	if got := s.Count(); got != 1 {
		t.Fatalf("history = %d, want the notification kept", got)
	}
}

func TestCloseDropsThePopupExceptOnExpiry(t *testing.T) {
	s := newTestService(t)
	id := s.Notify("app", 0, "", "one", "", nil, 0)
	s.Close(id, ClosedCall)
	if got := len(s.Popups()); got != 0 {
		t.Fatalf("popups = %d after close", got)
	}
	// Expired only removes the history entry; the popup has its own
	// countdown.
	id = s.Notify("app", 0, "", "two", "", nil, 0)
	s.Close(id, Expired)
	if got := len(s.Popups()); got != 1 {
		t.Fatalf("popups = %d after an expiry close, want the popup kept", got)
	}
}

func TestRemoveExpiredOffKeepsHistory(t *testing.T) {
	s := newTestService(t)
	s.SetRemoveExpired(false)
	s.Notify("app", 0, "", "flash", "", nil, 20)
	time.Sleep(60 * time.Millisecond)
	if got := s.Count(); got != 1 {
		t.Fatalf("history = %d, want expired entries kept with remove-expired off", got)
	}
}

func TestEverySubscriberSeesEveryEvent(t *testing.T) {
	s := newTestService(t)
	a, _ := s.Subscribe()
	b, _ := s.Subscribe()
	s.Notify("app", 0, "", "one", "", nil, 0)
	for name, feed := range map[string]<-chan Event{"a": a, "b": b} {
		select {
		case ev := <-feed:
			if ev.Kind != EventAdd {
				t.Errorf("%s: event = %+v", name, ev)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %s missed the event", name)
		}
	}
}
