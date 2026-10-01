// Package lock is the secure lock screen (wayle-shell's shell/lock): an
// ext-session-lock surface per output, so the compositor blanks every
// output, routes input only here, and keeps the session locked if the
// shell dies. The password is verified by a PAM conversation on a
// worker goroutine (service/auth), so the event loop is never stalled.
//
// Triggers: logind's Lock and Unlock signals (loginctl lock-session,
// idle daemons), the com.wayle.Shell1 Lock method (`wayle lock`,
// wayle-lock), lock-on-start for autologin sessions, and the LockedHint
// restart recovery. The screen reports its state back with
// SetLockedHint.
package lock

import (
	"image"
	"log"
	"strconv"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/auth"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/styling"
)

// The lock-screen strings (wayle-shell-core's _lock.ftl, en-US).
const (
	msgIncorrect = "Incorrect password"
	msgLockedOut = "Too many failed attempts"
)

func msgFailedAttempts(n uint32) string {
	return "Incorrect password — " + strconv.FormatUint(uint64(n), 10) + " failed attempts"
}

// blackout is .lock-scrim: the opaque blank-screen layer.
var blackout = render.RGB(0, 0, 0)

// locker is the session-lock surface the screen drives; appLocker
// adapts gelm's, tests substitute a fake.
type locker interface {
	// Available reports ext-session-lock-v1 support.
	Available() bool
	// Active reports a lock pending or held.
	Active() bool
	// Lock requests the session lock; cfg.Surface builds each output's
	// surface.
	Lock(cfg app.SessionLockConfig) error
	// Release unlocks a held lock or withdraws a pending one.
	Release()
	// Focus moves keyboard focus to w in the surface holding it.
	Focus(w widget.Widget)
}

// loop is the event loop the screen's timers and cross-goroutine
// events run on.
type loop interface {
	Invoke(func())
	Every(time.Duration, func()) (cancel func())
}

// Deps are the screen's collaborators; New fills the production ones.
type Deps struct {
	Locker locker
	Loop   loop
	// Now is the clock the grace window and the labels read.
	Now func() time.Time
	// After runs fn on the loop after d, once; cancel stops it.
	After func(d time.Duration, fn func()) (cancel func())
	// Auth starts a conversation; the production one is PAM against
	// the configured service for the session user.
	Auth func(service string, onEvent func(auth.Event)) authHandle
	// SetLockedHint reports the state to logind, best effort; nil
	// without logind.
	SetLockedHint func(locked bool)
	// LoadImage decodes (and blurs) the background image.
	LoadImage func(path string, blur uint32) (image.Image, error)
}

// authHandle is the running conversation's reply side.
type authHandle interface {
	Answer(string)
	Cancel()
}

// face is one output's lock surface: the prompt and the blank scrim.
type face struct {
	prompt *credential.Prompt
	scrim  *credential.Fill
}

// Screen is the lock screen component. Every method runs on the event
// loop; auth events and signals cross into it through Loop.Invoke.
type Screen struct {
	cfg       config.LockConfig
	wallpaper string
	fonts     credential.Fonts
	pal       *styling.Palette
	d         Deps

	faces []*face
	// bg is the background decoded once per lock, shared by every
	// output's surface; nil draws the color.
	bg image.Image
	// attempts counts failures in this lock session.
	attempts uint32
	// lockedAt drives the password-free grace window.
	lockedAt time.Time
	// auth is the running conversation, nil when idle; authGen tags
	// its events so a stale conversation's late events are dropped.
	auth    authHandle
	authGen uint64
	// awaiting: a prompt is on screen waiting for the next submit.
	awaiting bool
	// pending answers the conversation's first prompt: the password
	// typed before PAM asked for it.
	pending   *string
	inputOn   bool
	message   string
	blanked   bool
	clockStop func()
	blankStop func()
	blankGen  uint64
	clockText string
	dateText  string
	// confirmed: the compositor sent locked for this lock.
	confirmed bool
}

// New builds the screen over its collaborators.
func New(cfg *config.Config, fonts credential.Fonts, pal *styling.Palette, d Deps) *Screen {
	return &Screen{cfg: cfg.Lock, wallpaper: cfg.Wallpaper.Wallpaper, fonts: fonts, pal: pal, d: d}
}

// SetConfig applies a reloaded config, as the Rust lock reads its keys
// live: every later read (the grace period, the attempt limit, the
// PAM service, the blank timeout, the clock formats) sees it at once,
// and the next lock builds its surfaces and background from it. Loop
// goroutine.
func (s *Screen) SetConfig(cfg *config.Config) {
	s.cfg, s.wallpaper = cfg.Lock, cfg.Wallpaper.Wallpaper
}

// Locked reports whether a lock is pending or held.
func (s *Screen) Locked() bool { return s.d.Locker.Active() }

// Lock acquires the session lock and covers every output (idempotent
// while locked). Refused, with a log line, when [lock] is disabled or
// the compositor lacks ext-session-lock-v1.
func (s *Screen) Lock() {
	if !s.cfg.Enabled {
		log.Printf("lock: disabled in config; ignoring lock request")
		return
	}
	if s.d.Locker.Active() {
		return
	}
	if !s.d.Locker.Available() {
		log.Printf("lock: compositor does not support ext-session-lock-v1; cannot lock")
		return
	}
	s.bg = s.loadBackground()
	s.attempts = 0
	s.resetAuth()
	s.lockedAt = s.d.Now()
	s.inputOn = true
	s.message = ""
	s.blanked = false
	s.confirmed = false
	s.refreshClockText()
	err := s.d.Locker.Lock(app.SessionLockConfig{
		Surface:    s.buildFace,
		OnLocked:   s.onLocked,
		OnFinished: s.onFinished,
	})
	if err != nil {
		log.Printf("lock: cannot lock the session: %v", err)
		s.faces = nil
		return
	}
	s.startClock()
	s.armBlank()
}

// loadBackground resolves and decodes the configured background.
func (s *Screen) loadBackground() image.Image {
	path := s.cfg.Background.ImagePath(s.wallpaper)
	if path == "" {
		return nil
	}
	img, err := s.d.LoadImage(path, s.cfg.Blur)
	if err != nil {
		log.Printf("lock: background %s unreadable; using the color fill: %v", path, err)
		return nil
	}
	return img
}

// buildFace builds one output's surface; gelm calls it for every
// output at lock time and again for each output plugged in while
// locked. A new face picks up the current state.
func (s *Screen) buildFace(*app.Output) app.LockSurface {
	f := &face{scrim: credential.NewFill(blackout)}
	f.prompt = credential.Build(credential.Options{
		Fonts:     s.fonts,
		Palette:   s.pal,
		ShowClock: s.cfg.ShowClock,
	}, s.Submit)
	f.prompt.Clock.SetText(s.clockText)
	f.prompt.Date.SetText(s.dateText)
	f.prompt.SetMessage(s.message)
	f.prompt.Entry.SetEnabled(s.inputOn)
	f.scrim.SetOn(s.blanked)
	s.faces = append(s.faces, f)
	root := widget.NewOverlay().
		Append(credential.Background(s.bg, credential.HexFill(s.cfg.Background.Color))).
		Append(f.prompt.Root).
		Append(f.scrim)
	return app.LockSurface{
		Root:       root,
		Background: blackout,
		Focus:      f.prompt.Entry,
		// Any key press is activity: unblank and re-arm the timer.
		OnKey:    func(*widget.Router, uint32, app.Mods) { s.Activity() },
		OnClosed: func() { s.dropFace(f) },
	}
}

// dropFace forgets the surface of an unplugged output (or of an ended
// lock).
func (s *Screen) dropFace(f *face) {
	for i, cur := range s.faces {
		if cur == f {
			s.faces = append(s.faces[:i], s.faces[i+1:]...)
			return
		}
	}
}

func (s *Screen) onLocked() {
	s.confirmed = true
	s.setLockedHint(true)
	log.Printf("lock: session locked (%d monitors)", len(s.faces))
}

// onFinished: the compositor refused the lock (a previous lock client
// still holds the session) or ended a held one by its own policy.
func (s *Screen) onFinished() {
	if s.confirmed {
		log.Printf("lock: the compositor ended the session lock")
		s.setLockedHint(false)
	} else {
		log.Printf("lock: compositor denied the session lock (a previous lock client " +
			"may still hold the session); on Hyprland set " +
			"misc:allow_session_lock_restore = true to allow takeover")
	}
	s.confirmed = false
	s.stopTimers()
	s.resetAuth()
	s.faces = nil
	s.attempts = 0
	s.lockedAt = time.Time{}
}

// ForceUnlock is logind's Unlock: tear down without a password - only
// something that already authorized it (logind, a PAM agent) sends it.
func (s *Screen) ForceUnlock() { s.release() }

// Submit handles one Enter in a password entry. The first submit of a
// lock session starts a PAM conversation and stashes the value for its
// first prompt; a submit while a later prompt is on screen (an expired
// password, an OTP) answers it.
func (s *Screen) Submit(value string) {
	if s.exhausted() {
		return
	}
	if s.awaiting {
		s.awaiting = false
		if s.auth != nil {
			s.auth.Answer(value)
		}
		s.setInput(false)
		return
	}
	if s.auth != nil {
		return // a conversation runs and asked nothing: a stray submit
	}
	if grace := s.cfg.GracePeriodMS; grace > 0 && !s.lockedAt.IsZero() &&
		s.d.Now().Sub(s.lockedAt) < time.Duration(grace)*time.Millisecond {
		log.Printf("lock: unlocked within grace window")
		s.release()
		return
	}
	if value == "" {
		return
	}
	s.setInput(false)
	s.startConversation(value)
}

func (s *Screen) startConversation(first string) {
	s.pending = &first
	s.authGen++
	gen := s.authGen
	s.auth = s.d.Auth(s.cfg.PamService, func(ev auth.Event) {
		s.d.Loop.Invoke(func() {
			if gen == s.authGen {
				s.onAuthEvent(ev)
			}
		})
	})
}

func (s *Screen) onAuthEvent(ev auth.Event) {
	switch ev.Kind {
	case auth.EventPrompt:
		s.onPrompt(ev.Prompt)
	case auth.EventSuccess:
		log.Printf("lock: authentication succeeded")
		s.release()
	case auth.EventFailure:
		s.onFailure(ev.Reason)
	}
}

// onPrompt answers input prompts from the stashed value when there is
// one, else re-enables the entry for the user; info and error prompts
// only show their text.
func (s *Screen) onPrompt(p auth.Prompt) {
	if !p.WantsInput() {
		s.setMessage(p.Text)
		return
	}
	if s.pending != nil {
		answer := *s.pending
		s.pending = nil
		if s.auth != nil {
			s.auth.Answer(answer)
		}
		return
	}
	s.awaiting = true
	s.setInput(true)
	s.clearEntries()
	s.focusEntry()
}

// onFailure counts the attempt and re-arms the entry unless the cap is
// reached.
func (s *Screen) onFailure(reason string) {
	s.resetAuth()
	if s.attempts < ^uint32(0) {
		s.attempts++
	}
	log.Printf("lock: authentication failed (attempts %d): %s", s.attempts, reason)
	s.setInput(!s.exhausted())
	s.setMessage(s.failureText())
	s.clearEntries()
	if !s.exhausted() {
		s.focusEntry()
	}
}

func (s *Screen) failureText() string {
	switch {
	case s.exhausted():
		return msgLockedOut
	case s.cfg.ShowFailedAttempts:
		return msgFailedAttempts(s.attempts)
	}
	return msgIncorrect
}

// exhausted reports the configured attempt cap reached (0 is no cap).
func (s *Screen) exhausted() bool {
	return s.cfg.MaxAttempts > 0 && s.attempts >= s.cfg.MaxAttempts
}

// resetAuth drops the running conversation (a blocked PAM prompt sees
// a cancellation) and its transient state.
func (s *Screen) resetAuth() {
	if s.auth != nil {
		s.auth.Cancel()
	}
	s.auth = nil
	s.authGen++
	s.awaiting = false
	s.pending = nil
}

// release tears the surfaces down and ends the lock.
func (s *Screen) release() {
	s.stopTimers()
	if s.d.Locker.Active() {
		s.d.Locker.Release()
	}
	s.confirmed = false
	s.faces = nil
	s.lockedAt = time.Time{}
	s.attempts = 0
	s.resetAuth()
	s.setLockedHint(false)
	log.Printf("lock: session unlocked")
}

func (s *Screen) setLockedHint(locked bool) {
	if s.d.SetLockedHint != nil {
		s.d.SetLockedHint(locked)
	}
}

// Activity unblanks and re-arms the blank timer.
func (s *Screen) Activity() {
	s.setBlanked(false)
	s.armBlank()
}

func (s *Screen) armBlank() {
	if s.blankStop != nil {
		s.blankStop()
		s.blankStop = nil
	}
	s.blankGen++
	ms := s.cfg.BlankTimeoutMS
	if ms == 0 || !s.d.Locker.Active() {
		return
	}
	gen := s.blankGen
	s.blankStop = s.d.After(time.Duration(ms)*time.Millisecond, func() {
		if gen == s.blankGen {
			s.setBlanked(true)
		}
	})
}

func (s *Screen) setBlanked(on bool) {
	s.blanked = on
	for _, f := range s.faces {
		f.scrim.SetOn(on)
	}
}

func (s *Screen) startClock() {
	s.refreshClock()
	s.clockStop = s.d.Loop.Every(time.Second, s.refreshClock)
}

func (s *Screen) refreshClockText() {
	now := s.d.Now()
	s.clockText = s.cfg.ClockFormat.Layout().Format(now)
	s.dateText = s.cfg.DateFormat.Layout().Format(now)
}

func (s *Screen) refreshClock() {
	s.refreshClockText()
	for _, f := range s.faces {
		f.prompt.Clock.SetText(s.clockText)
		f.prompt.Date.SetText(s.dateText)
	}
}

func (s *Screen) stopTimers() {
	if s.clockStop != nil {
		s.clockStop()
		s.clockStop = nil
	}
	if s.blankStop != nil {
		s.blankStop()
		s.blankStop = nil
	}
	s.blankGen++
	s.blanked = false
}

func (s *Screen) setInput(on bool) {
	s.inputOn = on
	for _, f := range s.faces {
		f.prompt.Entry.SetEnabled(on)
	}
}

func (s *Screen) setMessage(text string) {
	s.message = text
	for _, f := range s.faces {
		f.prompt.SetMessage(text)
	}
}

func (s *Screen) clearEntries() {
	for _, f := range s.faces {
		f.prompt.Entry.SetText("")
	}
}

// focusEntry puts keyboard focus back on every surface's entry (each
// surface keeps its own focus; the compositor picks the surface).
func (s *Screen) focusEntry() {
	for _, f := range s.faces {
		s.d.Locker.Focus(f.prompt.Entry)
	}
}
