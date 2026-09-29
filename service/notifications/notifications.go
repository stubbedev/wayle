// Package notifications is the desktop notification service: the
// org.freedesktop.Notifications server, the stored history, popups
// with expiry, and the sticky do-not-disturb state.
package notifications

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/wayle/internal/glob"
)

// D-Bus identity and server information.
const (
	Interface     = "org.freedesktop.Notifications"
	ObjPath       = "/org/freedesktop/Notifications"
	ServerName    = "wayle"
	ServerVendor  = "wayle"
	ServerVersion = "0.8.53"
	SpecVersion   = "1.2"
)

// Capabilities the server advertises (daemon.rs).
var Capabilities = []string{
	"body",
	"body-markup",
	"actions",
	"icon-static",
	"persistence",
}

// ClosedReason is the spec's closed reason codes.
type ClosedReason uint32

// Closed reasons.
const (
	Expired    ClosedReason = 1
	Dismissed  ClosedReason = 2
	ClosedCall ClosedReason = 3
	Unknown    ClosedReason = 4
)

// Notification is one received notification.
type Notification struct {
	ID       uint32
	AppName  string
	AppIcon  string
	Summary  string
	Body     string
	Actions  []string
	Expires  time.Time // zero: never
	ExpireMS int32
	Added    time.Time
}

// Expired reports whether the expiry elapsed at now.
func (n *Notification) Expired(now time.Time) bool {
	return !n.Expires.IsZero() && now.After(n.Expires)
}

// Event is the service's change feed.
type Event struct {
	Kind   EventKind
	Notif  *Notification
	ID     uint32
	Reason ClosedReason
}

// Event kinds.
const (
	EventAdd    EventKind = "add"
	EventRemove EventKind = "remove"
	EventDnd    EventKind = "dnd"
	EventAction EventKind = "action"
)

// EventKind names a change.
type EventKind string

// Service holds the state and serves the D-Bus interface.
type Service struct {
	mu            sync.Mutex
	next          uint32
	all           []*Notification
	popups        map[uint32]*time.Timer
	dnd           bool
	removeExpired bool
	block         []string
	events        chan Event
	owners        map[uint32]string
	emit          func(signal string, args ...any)
}

// NewService restores the DND flag from the state dir.
func NewService() *Service {
	return &Service{
		next:          0,
		popups:        make(map[uint32]*time.Timer),
		dnd:           loadDND(),
		removeExpired: true,
		events:        make(chan Event, 32),
		owners:        make(map[uint32]string),
	}
}

// SetEmitter wires the D-Bus signal callback (server-side only).
func (s *Service) SetEmitter(fn func(signal string, args ...any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit = fn
}

// Events ticks on every change.
func (s *Service) Events() <-chan Event { return s.events }

// notify drops an event, coalescing when the buffer is full.
func (s *Service) notify(ev Event) {
	select {
	case s.events <- ev:
	default:
	}
}

// Notifications snapshots the stored history.
func (s *Service) Notifications() []*Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Notification, len(s.all))
	copy(out, s.all)
	return out
}

// Count returns the stored count.
func (s *Service) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.all)
}

// Popups snapshots the visible popups: none while DND is on.
func (s *Service) Popups() []*Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dnd {
		return nil
	}
	now := time.Now()
	var out []*Notification
	for _, n := range s.all {
		if !n.Expired(now) {
			out = append(out, n)
		}
	}
	return out
}

// DND reports do-not-disturb.
func (s *Service) DND() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dnd
}

// SetDND flips do-not-disturb and persists it.
func (s *Service) SetDND(on bool) {
	s.mu.Lock()
	s.dnd = on
	s.mu.Unlock()
	saveDND(on)
	s.notify(Event{Kind: EventDnd})
}

// ToggleDND flips do-not-disturb, returning the new state.
func (s *Service) ToggleDND() bool {
	s.mu.Lock()
	s.dnd = !s.dnd
	on := s.dnd
	s.mu.Unlock()
	saveDND(on)
	s.notify(Event{Kind: EventDnd})
	return on
}

// SetRemoveExpired toggles auto-removal of expired history entries.
func (s *Service) SetRemoveExpired(on bool) {
	s.mu.Lock()
	s.removeExpired = on
	s.mu.Unlock()
}

// SetBlocklist replaces the app-name glob blocklist.
func (s *Service) SetBlocklist(patterns []string) {
	s.mu.Lock()
	s.block = patterns
	s.mu.Unlock()
}

// Notify receives a notification (the D-Bus Notify call). A blocked
// app name consumes the notification and still returns an id.
func (s *Service) Notify(appName string, replacesID uint32, appIcon, summary, body string, actions []string, expireTimeout int32) uint32 {
	s.mu.Lock()
	if replacesID == 0 {
		s.next++
		replacesID = s.next
	} else {
		s.remove(replacesID)
		if replacesID > s.next {
			s.next = replacesID
		}
	}
	for _, pattern := range s.block {
		if globMatch(pattern, appName) {
			s.mu.Unlock()
			return replacesID
		}
	}
	n := &Notification{
		ID:       replacesID,
		AppName:  appName,
		AppIcon:  appIcon,
		Summary:  summary,
		Body:     body,
		Actions:  actions,
		ExpireMS: expireTimeout,
		Added:    time.Now(),
	}
	if expireTimeout > 0 {
		n.Expires = time.Now().Add(time.Duration(expireTimeout) * time.Millisecond)
		s.startPopupTimerLocked(n)
	}
	s.all = append(s.all, n)
	s.owners[n.ID] = appName
	s.mu.Unlock()
	s.notify(Event{Kind: EventAdd, Notif: n})
	return n.ID
}

// Close removes a notification, emitting the closed signal.
func (s *Service) Close(id uint32, reason ClosedReason) {
	s.mu.Lock()
	removed := s.remove(id)
	s.mu.Unlock()
	if !removed {
		return
	}
	if s.emit != nil {
		s.emit(Interface+".NotificationClosed", id, uint32(reason))
	}
	s.notify(Event{Kind: EventRemove, ID: id, Reason: reason})
}

// DismissAll empties the history with the dismissed reason.
func (s *Service) DismissAll() {
	s.mu.Lock()
	ids := make([]uint32, 0, len(s.all))
	for _, n := range s.all {
		ids = append(ids, n.ID)
	}
	s.all = nil
	for id, timer := range s.popups {
		timer.Stop()
		delete(s.popups, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		if s.emit != nil {
			s.emit(Interface+".NotificationClosed", id, uint32(Dismissed))
		}
		s.notify(Event{Kind: EventRemove, ID: id, Reason: Dismissed})
	}
}

// InvokeAction emits ActionInvoked and closes the notification.
func (s *Service) InvokeAction(id uint32, key string) {
	s.mu.Lock()
	_, ok := s.owners[id]
	s.mu.Unlock()
	if !ok {
		return
	}
	if s.emit != nil {
		s.emit(Interface+".ActionInvoked", id, key)
	}
	s.Close(id, Dismissed)
	s.notify(Event{Kind: EventAction, ID: id})
}

// remove drops the notification under lock. The caller holds mu.
func (s *Service) remove(id uint32) bool {
	for i, n := range s.all {
		if n.ID == id {
			s.all = append(s.all[:i], s.all[i+1:]...)
			break
		}
	}
	if timer, ok := s.popups[id]; ok {
		timer.Stop()
		delete(s.popups, id)
	}
	delete(s.owners, id)
	return true
}

// startPopupTimerLocked schedules the expiry cleanup. The caller
// holds mu.
func (s *Service) startPopupTimerLocked(n *Notification) {
	timer := time.AfterFunc(time.Duration(n.ExpireMS)*time.Millisecond, func() {
		s.mu.Lock()
		removed := s.remove(n.ID)
		expire := s.removeExpired
		s.mu.Unlock()
		if !removed {
			return
		}
		if expire {
			if s.emit != nil {
				s.emit(Interface+".NotificationClosed", n.ID, uint32(Expired))
			}
			s.notify(Event{Kind: EventRemove, ID: n.ID, Reason: Expired})
		}
	})
	s.popups[n.ID] = timer
}

// globMatch delegates to the shared blocklist matcher.
func globMatch(pattern, name string) bool {
	return glob.Match(pattern, name)
}

// StateDir is the DND flag's directory ($XDG_STATE_HOME/wayle).
func StateDir() (string, bool) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "wayle"), true
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", false
	}
	return filepath.Join(home, ".local/state/wayle"), true
}

// loadDND restores the sticky flag; anything but "on" reads off.
func loadDND() bool {
	dir, ok := StateDir()
	if !ok {
		return false
	}
	body, err := os.ReadFile(filepath.Join(dir, "dnd")) //nolint:gosec // the path is wayle's own state dir
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(body)) == "on"
}

// saveDND mirrors the flag into the state dir; failures are quiet.
func saveDND(on bool) {
	dir, ok := StateDir()
	if !ok {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	body := "off\n"
	if on {
		body = "on\n"
	}
	_ = os.WriteFile(filepath.Join(dir, "dnd"), []byte(body), 0o600)
}

// String renders the summary for logs and tests.
func (n *Notification) String() string {
	return fmt.Sprintf("%d %s: %s", n.ID, n.AppName, n.Summary)
}
