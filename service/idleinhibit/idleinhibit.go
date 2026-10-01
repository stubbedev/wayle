// Package idleinhibit is the shared idle-inhibit service: reactive
// state (active, duration, remaining), the session-bus daemon the
// `wayle idle` CLI drives, and the client half of that interface.
package idleinhibit

import (
	"strconv"
	"sync"
	"time"

	"github.com/stubbedev/wayle/internal/feed"
)

// D-Bus identity.
const (
	ServiceName = "com.wayle.IdleInhibit1"
	ServicePath = "/com/wayle/IdleInhibit"
)

// State is the reactive state: whether inhibition runs, the stored
// duration in minutes (0 = indefinite), and the seconds left on the
// timer when one is running.
type State struct {
	mu            sync.Mutex
	active        bool
	durationMins  uint32
	remainingSecs int
	timerStop     chan struct{}
	changes       feed.Tick
}

// NewState starts with the given duration in minutes and inhibition
// off.
func NewState(durationMins uint32) *State {
	return &State{durationMins: durationMins}
}

// Active reports whether inhibition is on.
func (s *State) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// Duration returns the stored duration in minutes, 0 for indefinite.
func (s *State) Duration() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.durationMins
}

// Remaining returns the seconds left, 0 when inactive or indefinite.
func (s *State) Remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remainingSecs
}

// Indefinite reports the indefinite mode: active with no timer.
func (s *State) Indefinite() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && s.durationMins == 0
}

// Changes ticks after every state flip or timer second; every
// subscriber (the module on each output, the daemon) sees each one.
// stop ends the feed.
func (s *State) Changes() (<-chan struct{}, func()) { return s.changes.Subscribe() }

// notify ticks every subscriber, coalescing when one is pending.
func (s *State) notify() { feed.Notify(&s.changes) }

// Enable turns inhibition on. With indefinite the run has no timer;
// otherwise the stored duration (if any) counts down and auto-disables.
func (s *State) Enable(indefinite bool) {
	s.mu.Lock()
	s.stopTimerLocked()
	if !indefinite && s.durationMins > 0 {
		s.remainingSecs = int(s.durationMins * 60)
		s.startTimerLocked()
	} else {
		s.remainingSecs = 0
	}
	s.active = true
	s.mu.Unlock()
	s.notify()
}

// Disable turns inhibition off and clears the timer.
func (s *State) Disable() {
	s.mu.Lock()
	s.stopTimerLocked()
	s.active = false
	s.remainingSecs = 0
	s.mu.Unlock()
	s.notify()
}

// SetDuration stores the duration in minutes (0 = indefinite); an
// active run restarts on the new duration.
func (s *State) SetDuration(minutes uint32) {
	s.mu.Lock()
	s.durationMins = minutes
	if s.active {
		s.stopTimerLocked()
		if minutes == 0 {
			s.remainingSecs = 0
		} else {
			s.remainingSecs = int(minutes * 60)
			s.startTimerLocked()
		}
	}
	s.mu.Unlock()
	s.notify()
}

// AdjustDuration shifts the stored duration; negative clamps at zero.
func (s *State) AdjustDuration(deltaMinutes int32) {
	s.mu.Lock()
	current := max(int32(s.durationMins)+deltaMinutes, 0)
	s.mu.Unlock()
	s.SetDuration(uint32(current))
}

// AdjustRemaining shifts the seconds left by minutes: a positive
// delta caps at the stored duration, a negative one saturates at
// zero, and zero remaining disables the run. Inactive or indefinite
// runs ignore the call.
func (s *State) AdjustRemaining(deltaMinutes int32) {
	s.mu.Lock()
	if !s.active || s.durationMins == 0 {
		s.mu.Unlock()
		return
	}
	durationSecs := int(s.durationMins) * 60
	deltaSecs := int(deltaMinutes) * 60
	remaining := s.remainingSecs
	var next int
	if deltaSecs >= 0 {
		next = min(remaining+deltaSecs, durationSecs)
	} else {
		next = max(remaining+deltaSecs, 0)
	}
	if next == 0 {
		s.stopTimerLocked()
		s.active = false
		s.remainingSecs = 0
		s.mu.Unlock()
		s.notify()
		return
	}
	s.remainingSecs = next
	s.mu.Unlock()
	s.notify()
}

// SetRemaining replaces the seconds left, capped at the stored
// duration; zero disables the run. Inactive or indefinite runs
// ignore the call.
func (s *State) SetRemaining(minutes uint32) {
	s.mu.Lock()
	if !s.active || s.durationMins == 0 {
		s.mu.Unlock()
		return
	}
	next := min(int(minutes)*60, int(s.durationMins)*60)
	if next == 0 {
		s.stopTimerLocked()
		s.active = false
		s.remainingSecs = 0
		s.mu.Unlock()
		s.notify()
		return
	}
	s.remainingSecs = next
	s.mu.Unlock()
	s.notify()
}

// startTimerLocked runs the one-second countdown that auto-disables
// at zero. The caller holds mu.
func (s *State) startTimerLocked() {
	if s.timerStop != nil {
		return
	}
	stop := make(chan struct{})
	s.timerStop = stop
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.timerStop != stop || !s.active || s.remainingSecs <= 0 {
					s.mu.Unlock()
					return
				}
				s.remainingSecs--
				done := s.remainingSecs == 0
				if done {
					s.active = false
					s.remainingSecs = 0
					s.timerStop = nil
				}
				s.mu.Unlock()
				if done {
					s.notify()
					return
				}
				s.notify()
			}
		}
	}()
}

// stopTimerLocked cancels the countdown goroutine. The caller holds mu.
func (s *State) stopTimerLocked() {
	if s.timerStop != nil {
		close(s.timerStop)
		s.timerStop = nil
	}
}

// Err values surface through the daemon's error replies.
var _ = struct{}{}

// FormatDuration renders seconds as H:MM:SS above the hour and M:SS
// within it (helpers.rs's format_duration).
func FormatDuration(totalSecs int) string {
	hours := totalSecs / 3600
	minutes := (totalSecs % 3600) / 60
	seconds := totalSecs % 60
	if hours > 0 {
		return strconv.Itoa(hours) + ":" + pad2(minutes) + ":" + pad2(seconds)
	}
	return strconv.Itoa(minutes) + ":" + pad2(seconds)
}

func pad2(v int) string {
	if v < 10 {
		return "0" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

// Snapshot is the status view the CLI prints.
type Snapshot struct {
	Active       bool
	DurationMins uint32
	RemainingS   int
}

// Status reads the current state as a snapshot.
func (s *State) Status() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{Active: s.active, DurationMins: s.durationMins, RemainingS: s.remainingSecs}
}
