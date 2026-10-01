// Package notifications is the desktop notification service: the
// org.freedesktop.Notifications server, the stored history, the popup
// list with its pausable countdowns, and the sticky do-not-disturb
// state.
package notifications

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/feed"
	"github.com/stubbedev/wayle/internal/glob"
	"github.com/stubbedev/wayle/internal/xdg"
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

// DefaultPopupDuration is the popup display time until the shell sets
// its configured one (builder.rs's popup_duration default).
const DefaultPopupDuration = 5000 * time.Millisecond

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
	ID      uint32
	AppName string
	AppIcon string
	Summary string
	Body    string
	// Actions are the parsed action pairs, the default one included.
	Actions []Action
	Expires time.Time // zero: never
	// ExpireMS is the sender's expire_timeout: negative is the server
	// default, zero never expires, positive is milliseconds.
	ExpireMS int32
	Added    time.Time
	Urgency  Urgency
	// ImagePath is the image-path hint, or the cached PNG of inline
	// image data.
	ImagePath    string
	DesktopEntry string
	// Transient notifications skip the history; resident ones stay
	// after an action is invoked.
	Transient bool
	Resident  bool
}

// DefaultAction is the "default" action, when the sender offered one.
func (n *Notification) DefaultAction() (Action, bool) {
	for _, a := range n.Actions {
		if a.ID == DefaultActionID {
			return a, true
		}
	}
	return Action{}, false
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
	// EventPopups is a popup-list change that leaves the history alone:
	// a countdown ran out or a popup was dismissed.
	EventPopups EventKind = "popups"
)

// EventKind names a change.
type EventKind string

// popupTimer is one popup's pausable countdown (popup_timer.rs's
// PopupTimer): running while timer is set, paused with the remaining
// time held in duration.
type popupTimer struct {
	started  time.Time
	duration time.Duration
	timer    *time.Timer
}

// remaining is the time left on a running countdown.
func (t *popupTimer) remaining(now time.Time) time.Duration {
	return max(t.duration-now.Sub(t.started), 0)
}

// Service holds the state and serves the D-Bus interface.
type Service struct {
	mu   sync.Mutex
	next uint32
	all  []*Notification
	// expiry holds the history-expiry timers (remove-expired).
	expiry map[uint32]*time.Timer
	// popups is the visible popup list, newest first; timers holds
	// each one's countdown.
	popups        []*Notification
	timers        map[uint32]*popupTimer
	popupDuration time.Duration
	dnd           bool
	removeExpired bool
	block         []string
	events        *feed.Feed[Event]
	owners        map[uint32]string
	emit          func(signal string, args ...any)
	// cacheImage turns inline image data into a file path.
	cacheImage func(imageData) (string, bool)
}

// NewService restores the DND flag from the state dir.
func NewService() *Service {
	return &Service{
		next:          0,
		expiry:        make(map[uint32]*time.Timer),
		timers:        make(map[uint32]*popupTimer),
		popupDuration: DefaultPopupDuration,
		dnd:           loadDND(),
		removeExpired: true,
		owners:        make(map[uint32]string),
		cacheImage:    cacheImage,
		events:        feed.New[Event](32),
	}
}

// SetEmitter wires the D-Bus signal callback (server-side only).
func (s *Service) SetEmitter(fn func(signal string, args ...any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit = fn
}

// Subscribe returns a new change feed. Every subscriber sees every
// event (the bar modules on each output and the popup host all follow
// the one service); a subscriber that falls behind drops events rather
// than stalling the service, and re-reads the snapshot on the next.
// stop ends the feed.
func (s *Service) Subscribe() (<-chan Event, func()) { return s.events.Subscribe() }

// notify fans an event out to every subscriber, dropping it for the
// ones whose buffer is full.
func (s *Service) notify(ev Event) { s.events.Publish(ev) }

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

// Popups snapshots the visible popup list, newest first. DND keeps
// new notifications off it (handle_popup_added); popups already shown
// when DND turns on stay until they time out or are dismissed.
func (s *Service) Popups() []*Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Notification, len(s.popups))
	copy(out, s.popups)
	return out
}

// SetPopupDuration sets how long a popup shows (set_popup_duration);
// it applies to popups that arrive afterwards.
func (s *Service) SetPopupDuration(d time.Duration) {
	s.mu.Lock()
	s.popupDuration = d
	s.mu.Unlock()
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

// Notify receives a notification without hints; see NotifyHints.
func (s *Service) Notify(appName string, replacesID uint32, appIcon, summary, body string, actions []string, expireTimeout int32) uint32 {
	return s.NotifyHints(appName, replacesID, appIcon, summary, body, actions, nil, expireTimeout)
}

// NotifyHints receives a notification (the D-Bus Notify call). An app
// may only replace an id it owns; any other replaces_id gets a new id
// (resolve_id). A blocked app name consumes the notification and still
// returns an id. A transient notification pops up but skips the
// history (handle_notification_added); the history is newest first.
func (s *Service) NotifyHints(appName string, replacesID uint32, appIcon, summary, body string, actions []string, rawHints map[string]dbus.Variant, expireTimeout int32) uint32 {
	h := decodeHints(rawHints, s.cacheImage)
	s.mu.Lock()
	id := replacesID
	if id == 0 || s.owners[id] != appName {
		s.next++
		id = s.next
	}
	for _, pattern := range s.block {
		if globMatch(pattern, appName) {
			s.mu.Unlock()
			return id
		}
	}
	s.removeHistoryLocked(id)
	n := &Notification{
		ID:           id,
		AppName:      appName,
		AppIcon:      appIcon,
		Summary:      summary,
		Body:         body,
		Actions:      ParseActions(actions),
		ExpireMS:     expireTimeout,
		Added:        time.Now(),
		Urgency:      h.urgency,
		ImagePath:    h.imagePath,
		DesktopEntry: h.desktopEntry,
		Transient:    h.transient,
		Resident:     h.resident,
	}
	if expireTimeout > 0 {
		n.Expires = n.Added.Add(time.Duration(expireTimeout) * time.Millisecond)
	}
	if !n.Transient {
		s.all = append([]*Notification{n}, s.all...)
		if !n.Expires.IsZero() && s.removeExpired {
			s.startExpiryLocked(n)
		}
	}
	s.owners[n.ID] = appName
	s.addPopupLocked(n)
	s.mu.Unlock()
	s.notify(Event{Kind: EventAdd, Notif: n})
	return n.ID
}

// addPopupLocked is handle_popup_added: outside DND the notification
// goes to the front of the popup list (a replacement moves up) and
// gets its countdown — the popup duration, capped by the sender's
// positive expire_timeout; a zero timeout sticks until dismissed. The
// caller holds mu.
func (s *Service) addPopupLocked(n *Notification) {
	if s.dnd {
		return
	}
	s.dropPopupLocked(n.ID)
	s.popups = append([]*Notification{n}, s.popups...)
	switch {
	case n.ExpireMS == 0:
	case n.ExpireMS > 0:
		s.startPopupTimerLocked(n.ID, min(s.popupDuration, time.Duration(n.ExpireMS)*time.Millisecond))
	default:
		s.startPopupTimerLocked(n.ID, s.popupDuration)
	}
}

// Close removes a notification, emitting the closed signal. Every
// reason but Expired also takes it off the popup list; an expired
// history entry leaves its popup to its own countdown
// (handle_notification_removed).
func (s *Service) Close(id uint32, reason ClosedReason) {
	s.mu.Lock()
	removed := s.removeHistoryLocked(id)
	if reason != Expired {
		s.dropPopupLocked(id)
	}
	emit := s.emit
	s.mu.Unlock()
	if !removed {
		return
	}
	if emit != nil {
		emit(Interface+".NotificationClosed", id, uint32(reason))
	}
	s.notify(Event{Kind: EventRemove, ID: id, Reason: reason})
}

// DismissAll empties the history and the popups with the dismissed
// reason.
func (s *Service) DismissAll() {
	s.mu.Lock()
	ids := make([]uint32, 0, len(s.all))
	for _, n := range s.all {
		ids = append(ids, n.ID)
	}
	for _, id := range ids {
		s.removeHistoryLocked(id)
		s.dropPopupLocked(id)
	}
	emit := s.emit
	s.mu.Unlock()
	for _, id := range ids {
		if emit != nil {
			emit(Interface+".NotificationClosed", id, uint32(Dismissed))
		}
		s.notify(Event{Kind: EventRemove, ID: id, Reason: Dismissed})
	}
}

// InvokeAction emits ActionInvoked and, unless the notification is
// resident, closes it with the closed reason (Notification::invoke).
func (s *Service) InvokeAction(id uint32, key string) {
	s.mu.Lock()
	_, ok := s.owners[id]
	n := s.findLocked(id)
	resident := n != nil && n.Resident
	emit := s.emit
	s.mu.Unlock()
	if !ok {
		return
	}
	if emit != nil {
		emit(Interface+".ActionInvoked", id, key)
	}
	if !resident {
		s.Close(id, ClosedCall)
	}
	s.notify(Event{Kind: EventAction, ID: id})
}

// DismissPopup hides a popup, keeping the notification in the history
// (dismiss_popup).
func (s *Service) DismissPopup(id uint32) {
	s.mu.Lock()
	dropped := s.dropPopupLocked(id)
	s.mu.Unlock()
	if dropped {
		s.notify(Event{Kind: EventPopups, ID: id})
	}
}

// InhibitPopup pauses a popup's countdown, keeping the time left
// (inhibit_popup: the pointer is over the card).
func (s *Service) InhibitPopup(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.timers[id]
	if !ok || t.timer == nil {
		return
	}
	t.timer.Stop()
	t.timer = nil
	t.duration = t.remaining(time.Now())
}

// ReleasePopup resumes a paused countdown with the time it had left,
// dropping the popup at once when none remained (release_popup). A
// running or unknown countdown is left alone.
func (s *Service) ReleasePopup(id uint32) {
	s.mu.Lock()
	t, ok := s.timers[id]
	if !ok || t.timer != nil {
		s.mu.Unlock()
		return
	}
	if t.duration <= 0 {
		dropped := s.dropPopupLocked(id)
		s.mu.Unlock()
		if dropped {
			s.notify(Event{Kind: EventPopups, ID: id})
		}
		return
	}
	s.startPopupTimerLocked(id, t.duration)
	s.mu.Unlock()
}

// startPopupTimerLocked (re)starts one popup's countdown; when it runs
// out the popup leaves the list, the history keeps the notification.
// The caller holds mu.
func (s *Service) startPopupTimerLocked(id uint32, d time.Duration) {
	if old, ok := s.timers[id]; ok && old.timer != nil {
		old.timer.Stop()
	}
	t := &popupTimer{started: time.Now(), duration: d}
	t.timer = time.AfterFunc(d, func() {
		s.mu.Lock()
		// A pause or restart since replaced this countdown.
		if s.timers[id] != t || t.timer == nil {
			s.mu.Unlock()
			return
		}
		dropped := s.dropPopupLocked(id)
		s.mu.Unlock()
		if dropped {
			s.notify(Event{Kind: EventPopups, ID: id})
		}
	})
	s.timers[id] = t
}

// dropPopupLocked removes a popup and its countdown, reporting whether
// it was on the list. The caller holds mu.
func (s *Service) dropPopupLocked(id uint32) bool {
	if t, ok := s.timers[id]; ok {
		if t.timer != nil {
			t.timer.Stop()
		}
		delete(s.timers, id)
	}
	for i, n := range s.popups {
		if n.ID == id {
			s.popups = append(s.popups[:i], s.popups[i+1:]...)
			return true
		}
	}
	return false
}

// removeHistoryLocked drops a notification from the history and its
// expiry timer, reporting whether it was stored. The caller holds mu.
func (s *Service) removeHistoryLocked(id uint32) bool {
	if timer, ok := s.expiry[id]; ok {
		timer.Stop()
		delete(s.expiry, id)
	}
	delete(s.owners, id)
	for i, n := range s.all {
		if n.ID == id {
			s.all = append(s.all[:i], s.all[i+1:]...)
			return true
		}
	}
	return false
}

// startExpiryLocked schedules the remove-expired cleanup: the history
// entry closes with the Expired reason once its timeout passes. The
// caller holds mu.
func (s *Service) startExpiryLocked(n *Notification) {
	var timer *time.Timer
	timer = time.AfterFunc(time.Until(n.Expires), func() {
		// A replacement under the same id stopped this timer; a fire
		// racing that Stop must not close the newcomer.
		s.mu.Lock()
		current := s.expiry[n.ID] == timer
		s.mu.Unlock()
		if current {
			s.Close(n.ID, Expired)
		}
	})
	s.expiry[n.ID] = timer
}

// globMatch delegates to the shared blocklist matcher.
func globMatch(pattern, name string) bool {
	return glob.Match(pattern, name)
}

// StateDir is the DND flag's directory, wayle's state dir.
func StateDir() (string, bool) { return xdg.StateDir() }

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

// findLocked is the stored or popped-up notification with id, or nil.
// The caller holds mu.
func (s *Service) findLocked(id uint32) *Notification {
	for _, list := range [][]*Notification{s.all, s.popups} {
		for _, n := range list {
			if n.ID == id {
				return n
			}
		}
	}
	return nil
}
