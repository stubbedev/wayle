package notifications

import (
	"testing"
	"time"
)

// collect waits for n events.
func collect(t *testing.T, s *Service, n int) []Event {
	t.Helper()
	var out []Event
	deadline := time.After(time.Second)
	for len(out) < n {
		select {
		case ev := <-s.Events():
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("got %d events, want %d", len(out), n)
		}
	}
	return out
}

// newTestService isolates the DND state dir per test.
func newTestService(t *testing.T) *Service {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return NewService()
}

func TestNotifyStoresAndEmits(t *testing.T) {
	s := newTestService(t)
	id := s.Notify("mail", 0, "", "New mail", "hello", nil, 0)
	if id == 0 {
		t.Fatal("ids start at one")
	}
	ev := collect(t, s, 1)[0]
	if ev.Kind != EventAdd || ev.Notif.ID != id || ev.Notif.Summary != "New mail" {
		t.Fatalf("event = %+v", ev)
	}
	if got := s.Count(); got != 1 {
		t.Fatalf("count = %d", got)
	}
	// IDs increment without reuse.
	if id2 := s.Notify("mail", 0, "", "Second", "", nil, 0); id2 != id+1 {
		t.Fatalf("second id = %d, want %d", id2, id+1)
	}
}

func TestReplacesID(t *testing.T) {
	s := newTestService(t)
	id := s.Notify("app", 0, "", "First", "", nil, 0)
	collect(t, s, 1)
	again := s.Notify("app", id, "", "Second", "", nil, 0)
	if again != id {
		t.Fatalf("replaces returned %d, want %d", again, id)
	}
	<-s.Events()
	if got := s.Count(); got != 1 {
		t.Fatalf("count = %d, want the replacement only", got)
	}
}

func TestBlocklistConsumes(t *testing.T) {
	s := newTestService(t)
	s.SetBlocklist([]string{"noisy*"})
	id := s.Notify("noisy-app", 0, "", "spam", "", nil, 0)
	if id == 0 {
		t.Fatal("blocked notifications still return an id")
	}
	select {
	case ev := <-s.Events():
		t.Fatalf("blocked notification leaked: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
	if got := s.Count(); got != 0 {
		t.Fatalf("count = %d, want 0", got)
	}
}

func TestDNDSuppressesPopups(t *testing.T) {
	s := newTestService(t)
	s.Notify("app", 0, "", "ping", "", nil, 0)
	<-s.Events()
	if len(s.Popups()) != 1 {
		t.Fatal("no dnd: the notification should be a popup")
	}
	s.SetDND(true)
	if got := len(s.Popups()); got != 0 {
		t.Fatalf("popups under dnd = %d", got)
	}
	if !s.DND() {
		t.Fatal("dnd flag lost")
	}
	// The dnd event arrives.
	for {
		select {
		case ev := <-s.Events():
			if ev.Kind == EventDnd {
				return
			}
		case <-time.After(time.Second):
			t.Fatal("no dnd event")
		}
	}
}

func TestExpiryRemoves(t *testing.T) {
	s := newTestService(t)
	s.Notify("app", 0, "", "flash", "", nil, 30)
	<-s.Events()
	deadline := time.Now().Add(2 * time.Second)
	for s.Count() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.Count(); got != 0 {
		t.Fatalf("expired notification stayed, count = %d", got)
	}
	sawExpired := false
	for {
		select {
		case ev := <-s.Events():
			if ev.Kind == EventRemove && ev.Reason == Expired {
				sawExpired = true
			}
		case <-time.After(100 * time.Millisecond):
			if !sawExpired {
				t.Fatal("no expired close event")
			}
			return
		}
	}
}

func TestZeroTimeoutNeverExpires(t *testing.T) {
	s := newTestService(t)
	s.Notify("app", 0, "", "sticky", "", nil, 0)
	<-s.Events()
	time.Sleep(60 * time.Millisecond)
	if got := s.Count(); got != 1 {
		t.Fatalf("expire_timeout 0 removed the notification")
	}
}

func TestDismissAll(t *testing.T) {
	s := newTestService(t)
	s.Notify("app", 0, "", "one", "", nil, 0)
	s.Notify("app", 0, "", "two", "", nil, 0)
	collect(t, s, 2)
	s.DismissAll()
	deadline := time.Now().Add(2 * time.Second)
	for s.Count() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.Count(); got != 0 {
		t.Fatalf("dismiss all left %d", got)
	}
}

func TestInvokeAction(t *testing.T) {
	s := newTestService(t)
	var signals []string
	s.SetEmitter(func(signal string, _ ...any) { signals = append(signals, signal) })
	id := s.Notify("app", 0, "", "hi", "", []string{"reply", "Reply"}, 0)
	<-s.Events()
	s.InvokeAction(id, "reply")
	if len(signals) == 0 || signals[0] != Interface+".ActionInvoked" {
		t.Fatalf("signals = %v", signals)
	}
	// Invoking an unknown id does nothing.
	s.InvokeAction(9999, "nope")
}

func TestGlobMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"*", "anything", true},
		{"noisy*", "noisy-app", true},
		{"noisy*", "quiet-app", false},
		{"*slack*", "Slack Desktop", true},
		{"exact", "exact", true},
		{"exact", "exactly", false},
	} {
		if got := globMatch(tc.pattern, tc.name); got != tc.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestDNDPersistence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := NewService()
	if s.DND() {
		t.Fatal("a fresh state dir starts with dnd off")
	}
	s.SetDND(true)
	restored := NewService()
	if !restored.DND() {
		t.Fatal("the dnd flag did not survive a restart")
	}
	s.SetDND(false)
	restored = NewService()
	if restored.DND() {
		t.Fatal("dnd off did not persist")
	}
}
