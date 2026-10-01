package idleinhibit

import (
	"testing"
	"time"
)

func waitChange(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no change tick within a second")
	}
}

func TestStateEnableTimed(t *testing.T) {
	s := NewState(2)
	changes, stop := s.Changes()
	defer stop()
	if s.Active() || s.Indefinite() {
		t.Fatal("a fresh state starts active")
	}
	s.Enable(false)
	if !s.Active() || s.Indefinite() {
		t.Fatalf("Enable(false) = active=%v indefinite=%v", s.Active(), s.Indefinite())
	}
	if got := s.Remaining(); got != 120 {
		t.Errorf("remaining = %d, want 120", got)
	}
	waitChange(t, changes)

	// The countdown runs and fires at zero, disabling the run: jump the
	// remaining time to its last second (white-box: the field lives in
	// this package) and let it tick out.
	s.mu.Lock()
	s.remainingSecs = 1
	s.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for s.Active() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.Active() {
		t.Fatal("the timer never fired")
	}
	if got := s.Duration(); got != 2 {
		t.Errorf("duration survived the run: %d", got)
	}
}

func TestStateEnableIndefinite(t *testing.T) {
	s := NewState(0)
	s.Enable(true)
	if !s.Active() || !s.Indefinite() {
		t.Fatalf("Enable(true) = active=%v indefinite=%v", s.Active(), s.Indefinite())
	}
	// Indefinite mode ignores timer adjustments.
	s.AdjustRemaining(5)
	if got := s.Remaining(); got != 0 {
		t.Errorf("AdjustRemaining in indefinite mode set remaining to %d", got)
	}
	s.SetRemaining(5)
	if got := s.Remaining(); got != 0 {
		t.Errorf("SetRemaining in indefinite mode set remaining to %d", got)
	}
	s.Disable()
	if s.Active() {
		t.Error("Disable did not stop the run")
	}
}

func TestStateDurationAdjustments(t *testing.T) {
	s := NewState(60)
	s.AdjustDuration(-90)
	if got := s.Duration(); got != 0 {
		t.Errorf("negative clamp = %d, want 0", got)
	}
	s.SetDuration(30)
	s.AdjustDuration(15)
	if got := s.Duration(); got != 45 {
		t.Errorf("duration = %d, want 45", got)
	}
}

func TestStateRemainingGuards(t *testing.T) {
	s := NewState(5)
	s.AdjustRemaining(1)
	if s.Active() || s.Remaining() != 0 {
		t.Errorf("inactive AdjustRemaining changed state: active=%v remaining=%d", s.Active(), s.Remaining())
	}
	s.Enable(false)
	if got := s.Remaining(); got != 300 {
		t.Fatalf("remaining = %d, want 300", got)
	}
	// Negative deltas subtract; saturating at zero disables the run.
	s.AdjustRemaining(-4)
	if got := s.Remaining(); got != 60 {
		t.Errorf("remaining after -4min = %d, want 60", got)
	}
	// Positive deltas cap at the full duration.
	s.AdjustRemaining(100)
	if got := s.Remaining(); got != 300 {
		t.Errorf("remaining after +100min = %d, want the 300 cap", got)
	}
	// Draining the remaining time disables the run.
	s.AdjustRemaining(-5)
	if s.Active() {
		t.Error("draining the remaining time did not disable the run")
	}
	// SetRemaining caps at the duration and zero disables.
	s.Enable(false)
	s.SetRemaining(99)
	if got := s.Remaining(); got != 300 {
		t.Errorf("SetRemaining above the duration = %d, want the cap", got)
	}
	s.SetRemaining(2)
	if got := s.Remaining(); got != 120 {
		t.Errorf("SetRemaining(2) = %d, want 120", got)
	}
	s.SetRemaining(0)
	if s.Active() {
		t.Error("SetRemaining(0) did not disable the run")
	}
	// SetDuration on a live run restarts it.
	s.Enable(false)
	s.SetDuration(3)
	if got := s.Remaining(); got != 180 {
		t.Errorf("remaining after SetDuration = %d, want 180", got)
	}
	// And to indefinite stops the timer outright.
	s.SetDuration(0)
	if got := s.Remaining(); got != 0 {
		t.Errorf("remaining after SetDuration(0) = %d, want 0", got)
	}
	s.Disable()
}

func TestStateTimerFiresToZero(t *testing.T) {
	s := NewState(1)
	s.Enable(false)
	// Jump to the last second; the tick takes it to zero and disables.
	s.mu.Lock()
	s.remainingSecs = 1
	s.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for s.Active() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.Active() {
		t.Fatal("the timer never fired")
	}
	if got := s.Duration(); got != 1 {
		t.Errorf("duration survived the run: %d", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		secs int
		want string
	}{
		{0, "0:00"},
		{59, "0:59"},
		{60, "1:00"},
		{3661, "1:01:01"},
		{3600, "1:00:00"},
		{7325, "2:02:05"},
	} {
		if got := FormatDuration(tc.secs); got != tc.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tc.secs, got, tc.want)
		}
	}
}
