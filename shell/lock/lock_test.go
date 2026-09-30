package lock

import (
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/auth"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/styling"
)

// fakeLocker plays gelm's session lock: Lock builds one surface per
// fake output right away, like LockSession covering every output.
type fakeLocker struct {
	available bool
	active    bool
	outputs   int
	cfg       app.SessionLockConfig
	surfaces  []app.LockSurface
	releases  int
	focused   []widget.Widget
	lockErr   error
}

func (f *fakeLocker) Available() bool { return f.available }
func (f *fakeLocker) Active() bool    { return f.active }

func (f *fakeLocker) Lock(cfg app.SessionLockConfig) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	f.active = true
	f.cfg = cfg
	for range f.outputs {
		f.plug()
	}
	return nil
}

// plug hotplugs one more output.
func (f *fakeLocker) plug() app.LockSurface {
	s := f.cfg.Surface(&app.Output{})
	f.surfaces = append(f.surfaces, s)
	return s
}

func (f *fakeLocker) Release() {
	f.active = false
	f.releases++
	for _, s := range f.surfaces {
		s.OnClosed()
	}
	f.surfaces = nil
}

func (f *fakeLocker) Focus(w widget.Widget) { f.focused = append(f.focused, w) }

// fakeLoop runs invokes inline and records the periodic timers.
type fakeLoop struct{ every []func() }

func (l *fakeLoop) Invoke(fn func()) { fn() }
func (l *fakeLoop) Every(_ time.Duration, fn func()) func() {
	l.every = append(l.every, fn)
	i := len(l.every) - 1
	return func() { l.every[i] = nil }
}

// fakeAuth records conversations; the test drives their events.
type fakeAuth struct {
	service   string
	onEvent   func(auth.Event)
	answers   []string
	cancelled bool
}

func (a *fakeAuth) Answer(s string) { a.answers = append(a.answers, s) }
func (a *fakeAuth) Cancel()         { a.cancelled = true }

type harness struct {
	s      *Screen
	locker *fakeLocker
	loop   *fakeLoop
	convs  []*fakeAuth
	hints  []bool
	now    time.Time
	timers []*timer
}

type timer struct {
	d       time.Duration
	fn      func()
	stopped bool
}

func newHarness(t *testing.T, edit func(*config.LockConfig)) *harness {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	if edit != nil {
		edit(&cfg.Lock)
	}
	h := &harness{
		locker: &fakeLocker{available: true, outputs: 2},
		loop:   &fakeLoop{},
		now:    time.Date(2026, 3, 5, 9, 7, 0, 0, time.Local),
	}
	h.s = New(cfg, credential.Fonts{Text: face, Clock: face}, styling.Default(), Deps{
		Locker: h.locker,
		Loop:   h.loop,
		Now:    func() time.Time { return h.now },
		After: func(d time.Duration, fn func()) func() {
			tm := &timer{d: d, fn: fn}
			h.timers = append(h.timers, tm)
			return func() { tm.stopped = true }
		},
		Auth: func(service string, onEvent func(auth.Event)) authHandle {
			c := &fakeAuth{service: service, onEvent: onEvent}
			h.convs = append(h.convs, c)
			return c
		},
		SetLockedHint: func(locked bool) { h.hints = append(h.hints, locked) },
		LoadImage: func(string, uint32) (image.Image, error) {
			return image.NewRGBA(image.Rect(0, 0, 4, 4)), nil
		},
	})
	return h
}

func (h *harness) lock(t *testing.T) {
	t.Helper()
	h.s.Lock()
	if !h.locker.active {
		t.Fatal("Lock did not acquire the session lock")
	}
	h.locker.cfg.OnLocked()
}

func (h *harness) conv(t *testing.T) *fakeAuth {
	t.Helper()
	if len(h.convs) == 0 {
		t.Fatal("no conversation started")
	}
	return h.convs[len(h.convs)-1]
}

func (h *harness) eachFace(t *testing.T, fn func(*face)) {
	t.Helper()
	if len(h.s.faces) == 0 {
		t.Fatal("no lock surfaces")
	}
	for _, f := range h.s.faces {
		fn(f)
	}
}

func TestLockCoversOutputsAndReportsTheHint(t *testing.T) {
	h := newHarness(t, nil)
	h.lock(t)
	if len(h.s.faces) != 2 {
		t.Fatalf("faces = %d, want one per output", len(h.s.faces))
	}
	h.eachFace(t, func(f *face) {
		if f.prompt.Clock.Text() != "09:07" || f.prompt.Date.Text() != "Thursday, March 5" {
			t.Errorf("clock = %q / %q", f.prompt.Clock.Text(), f.prompt.Date.Text())
		}
		if !f.prompt.Entry.Enabled() || f.prompt.Error.Visible() || f.scrim.On() {
			t.Error("a fresh surface must take input with no message and no blackout")
		}
	})
	for _, s := range h.locker.surfaces {
		if s.Focus == nil || s.Background.A() != 255 {
			t.Error("a lock surface must focus its entry and paint opaque")
		}
	}
	if len(h.hints) != 1 || !h.hints[0] {
		t.Errorf("LockedHint calls = %v, want [true] once locked", h.hints)
	}
	if len(h.loop.every) != 1 {
		t.Errorf("clock timers = %d, want 1", len(h.loop.every))
	}
	h.now = h.now.Add(time.Minute)
	h.loop.every[0]()
	if got := h.s.faces[1].prompt.Clock.Text(); got != "09:08" {
		t.Errorf("tick clock = %q", got)
	}

	h.s.Lock() // idempotent while locked
	if len(h.s.faces) != 2 || len(h.locker.surfaces) != 2 {
		t.Error("a second Lock while locked rebuilt the surfaces")
	}
}

func TestLockRefusals(t *testing.T) {
	h := newHarness(t, func(l *config.LockConfig) { l.Enabled = false })
	h.s.Lock()
	if h.locker.active {
		t.Error("[lock] enabled = false still locked")
	}
	h = newHarness(t, nil)
	h.locker.available = false
	h.s.Lock()
	if h.locker.active {
		t.Error("locked without ext-session-lock-v1")
	}
	h = newHarness(t, nil)
	h.locker.lockErr = errors.New("boom")
	h.s.Lock()
	if len(h.s.faces) != 0 || len(h.loop.every) != 0 {
		t.Error("a failed LockSession left state behind")
	}
}

// TestUnlockWithPassword: the typed password answers PAM's first
// prompt, success unlocks, and the hint goes false.
func TestUnlockWithPassword(t *testing.T) {
	h := newHarness(t, func(l *config.LockConfig) { l.PamService = "wayle" })
	h.lock(t)
	h.s.Submit("")
	if len(h.convs) != 0 {
		t.Fatal("an empty submit started a conversation")
	}
	h.s.Submit("hunter2")
	c := h.conv(t)
	if c.service != "wayle" {
		t.Errorf("conversation service = %q", c.service)
	}
	h.eachFace(t, func(f *face) {
		if f.prompt.Entry.Enabled() {
			t.Error("entries stay enabled while PAM runs")
		}
	})
	h.s.Submit("again") // a stray submit while PAM works
	if len(h.convs) != 1 || len(c.answers) != 0 {
		t.Fatal("a stray submit reached the conversation")
	}
	c.onEvent(auth.Event{Kind: auth.EventPrompt, Prompt: auth.Prompt{Kind: auth.PromptSecret, Text: "Password: "}})
	if len(c.answers) != 1 || c.answers[0] != "hunter2" {
		t.Fatalf("answers = %q, want the stashed password", c.answers)
	}
	c.onEvent(auth.Event{Kind: auth.EventSuccess})
	if h.locker.active || h.locker.releases != 1 {
		t.Error("success did not unlock")
	}
	if len(h.s.faces) != 0 || h.loop.every[0] != nil {
		t.Error("unlock left surfaces or the clock behind")
	}
	if got := h.hints[len(h.hints)-1]; got {
		t.Error("LockedHint not cleared on unlock")
	}
}

// TestFailedAttempts pins the failure feedback and the attempt cap.
func TestFailedAttempts(t *testing.T) {
	h := newHarness(t, func(l *config.LockConfig) { l.MaxAttempts = 2 })
	h.lock(t)
	fail := func() {
		t.Helper()
		h.s.Submit("wrong")
		h.conv(t).onEvent(auth.Event{Kind: auth.EventFailure, Reason: "authentication failed"})
	}
	fail()
	h.eachFace(t, func(f *face) {
		if f.prompt.Error.Text() != "Incorrect password — 1 failed attempts" || !f.prompt.Error.Visible() {
			t.Errorf("message = %q", f.prompt.Error.Text())
		}
		if !f.prompt.Entry.Enabled() || f.prompt.Entry.Text() != "" {
			t.Error("a failure must re-arm and clear the entry")
		}
	})
	if len(h.locker.focused) != 2 {
		t.Errorf("focus requests = %d, want the entry refocused on each surface", len(h.locker.focused))
	}
	fail()
	h.eachFace(t, func(f *face) {
		if f.prompt.Error.Text() != "Too many failed attempts" || f.prompt.Entry.Enabled() {
			t.Errorf("at the cap: message %q, entry enabled %v", f.prompt.Error.Text(), f.prompt.Entry.Enabled())
		}
	})
	h.s.Submit("more")
	if len(h.convs) != 2 {
		t.Error("a submit past the cap started a conversation")
	}
	if !h.locker.active {
		t.Error("the cap unlocked the screen; it must stay locked")
	}

	quiet := newHarness(t, func(l *config.LockConfig) { l.ShowFailedAttempts = false })
	quiet.lock(t)
	quiet.s.Submit("wrong")
	quiet.conv(t).onEvent(auth.Event{Kind: auth.EventFailure})
	if got := quiet.s.faces[0].prompt.Error.Text(); got != "Incorrect password" {
		t.Errorf("show-failed-attempts = false: %q", got)
	}
}

// TestRePrompt: a second input prompt (expired password, OTP) waits
// for the next submit; info prompts only show their text.
func TestRePrompt(t *testing.T) {
	h := newHarness(t, nil)
	h.lock(t)
	h.s.Submit("old")
	c := h.conv(t)
	c.onEvent(auth.Event{Kind: auth.EventPrompt, Prompt: auth.Prompt{Kind: auth.PromptSecret}})
	c.onEvent(auth.Event{Kind: auth.EventPrompt, Prompt: auth.Prompt{Kind: auth.PromptInfo, Text: "Password expired"}})
	if got := h.s.faces[0].prompt.Error.Text(); got != "Password expired" {
		t.Errorf("info prompt message = %q", got)
	}
	c.onEvent(auth.Event{Kind: auth.EventPrompt, Prompt: auth.Prompt{Kind: auth.PromptSecret, Text: "New password: "}})
	if !h.s.faces[0].prompt.Entry.Enabled() {
		t.Fatal("a re-prompt must re-enable the entry")
	}
	h.s.Submit("new")
	if len(c.answers) != 2 || c.answers[1] != "new" {
		t.Errorf("answers = %q, want the re-prompt answered by the next submit", c.answers)
	}
	if len(h.convs) != 1 {
		t.Error("answering a re-prompt started a new conversation")
	}
}

func TestGraceWindow(t *testing.T) {
	h := newHarness(t, func(l *config.LockConfig) { l.GracePeriodMS = 5000 })
	h.lock(t)
	h.now = h.now.Add(4 * time.Second)
	h.s.Submit("")
	if h.locker.active || len(h.convs) != 0 {
		t.Error("a submit within the grace window must unlock without PAM")
	}
	h = newHarness(t, func(l *config.LockConfig) { l.GracePeriodMS = 5000 })
	h.lock(t)
	h.now = h.now.Add(6 * time.Second)
	h.s.Submit("pw")
	if !h.locker.active || len(h.convs) != 1 {
		t.Error("past the grace window the password is required")
	}
}

func TestBlankTimer(t *testing.T) {
	h := newHarness(t, func(l *config.LockConfig) { l.BlankTimeoutMS = 30000 })
	h.lock(t)
	if len(h.timers) != 1 || h.timers[0].d != 30*time.Second {
		t.Fatalf("blank timers = %+v", h.timers)
	}
	h.timers[0].fn()
	h.eachFace(t, func(f *face) {
		if !f.scrim.On() {
			t.Error("the blank timer did not black out the surfaces")
		}
	})
	h.s.Activity()
	h.eachFace(t, func(f *face) {
		if f.scrim.On() {
			t.Error("activity did not unblank")
		}
	})
	if len(h.timers) != 2 || h.timers[1].d != 30*time.Second {
		t.Error("activity did not re-arm the blank timer")
	}
	h.timers[0].fn() // a stale timer must not blank
	if h.s.faces[0].scrim.On() {
		t.Error("a superseded blank timer blanked the screen")
	}

	never := newHarness(t, nil)
	never.lock(t)
	if len(never.timers) != 0 {
		t.Error("blank-timeout-ms = 0 armed a timer")
	}
}

// TestHotplugInheritsState: an output plugged in mid-lock gets a
// surface showing the current message and input state.
func TestHotplugInheritsState(t *testing.T) {
	h := newHarness(t, nil)
	h.lock(t)
	h.s.Submit("wrong")
	h.conv(t).onEvent(auth.Event{Kind: auth.EventFailure})
	h.s.Submit("again")
	h.locker.plug()
	late := h.s.faces[len(h.s.faces)-1]
	if late.prompt.Error.Text() != "Incorrect password — 1 failed attempts" {
		t.Errorf("hotplugged message = %q", late.prompt.Error.Text())
	}
	if late.prompt.Entry.Enabled() {
		t.Error("a hotplugged surface took input while PAM runs")
	}
	h.locker.surfaces[0].OnClosed() // unplug the first output
	if len(h.s.faces) != 2 {
		t.Errorf("faces after unplug = %d, want 2", len(h.s.faces))
	}
}

func TestStaleConversationIgnored(t *testing.T) {
	h := newHarness(t, nil)
	h.lock(t)
	h.s.Submit("pw")
	stale := h.conv(t)
	h.s.ForceUnlock()
	if h.locker.active || !stale.cancelled {
		t.Fatal("ForceUnlock must unlock and cancel the conversation")
	}
	h.lock(t)
	stale.onEvent(auth.Event{Kind: auth.EventFailure})
	if h.s.attempts != 0 {
		t.Error("a stale conversation's failure counted against the new lock")
	}
	stale.onEvent(auth.Event{Kind: auth.EventSuccess})
	if !h.locker.active {
		t.Error("a stale conversation's success unlocked the new lock")
	}
}

func TestDeniedLock(t *testing.T) {
	h := newHarness(t, nil)
	h.s.Lock()
	h.locker.active = false // gelm drops a finished lock
	h.locker.cfg.OnFinished()
	if len(h.s.faces) != 0 || h.loop.every[0] != nil {
		t.Error("a denied lock left surfaces or the clock behind")
	}
	if len(h.hints) != 0 {
		t.Errorf("a denied lock touched LockedHint: %v", h.hints)
	}
}

func TestBackgroundModes(t *testing.T) {
	var loaded []string
	h := newHarness(t, func(l *config.LockConfig) {
		l.Background.Mode = config.BackgroundImage
		l.Background.Image = "/srv/lock.png"
		l.Blur = 8
	})
	h.s.d.LoadImage = func(path string, blur uint32) (image.Image, error) {
		loaded = append(loaded, path)
		if blur != 8 {
			t.Errorf("blur = %d", blur)
		}
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
	}
	h.lock(t)
	if len(loaded) != 1 || loaded[0] != "/srv/lock.png" || h.s.bg == nil {
		t.Errorf("image mode loaded %q (decoded once for every output)", loaded)
	}

	h = newHarness(t, nil)
	h.s.d.LoadImage = func(string, uint32) (image.Image, error) {
		t.Error("color mode decoded an image")
		return nil, nil
	}
	h.lock(t)

	h = newHarness(t, func(l *config.LockConfig) { l.Background.Mode = config.BackgroundImage; l.Background.Image = "/nope" })
	h.s.d.LoadImage = func(string, uint32) (image.Image, error) { return nil, errors.New("no such file") }
	h.lock(t)
	if h.s.bg != nil || !h.locker.active {
		t.Error("an unreadable image must fall back to the color and still lock")
	}
}

func TestClaimLockOnStart(t *testing.T) {
	dir := t.TempDir()
	if !claimLockOnStart(dir) {
		t.Fatal("the first start of a session must lock")
	}
	if second, third := claimLockOnStart(dir), claimLockOnStart(dir); second || third {
		t.Error("a restart within the session must not lock")
	}
	if !claimLockOnStart("") {
		t.Error("without a runtime dir it must fail secure and lock")
	}
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 && !claimLockOnStart(ro) {
		t.Error("an unwritable runtime dir must fail secure and lock")
	}
}

func TestShouldRelock(t *testing.T) {
	if !shouldRelock(true, nil) {
		t.Error("LockedHint set: the previous shell died holding the lock, re-acquire")
	}
	if shouldRelock(false, nil) {
		t.Error("LockedHint clear: a restart must not lock")
	}
	if shouldRelock(true, errors.New("no logind")) {
		t.Error("probe unavailable: fail soft")
	}
}

func TestFailureTextCount(t *testing.T) {
	if got := msgFailedAttempts(3); !strings.Contains(got, "3 failed attempts") {
		t.Errorf("%q", got)
	}
}
