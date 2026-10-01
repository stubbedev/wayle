package greeter

import (
	"errors"
	"image"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/auth"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/styling"
)

// fakeHandle is one greetd conversation the screen spawned.
type fakeHandle struct {
	username  string
	answers   []string
	cancelled bool
	emit      func(auth.Event)
}

func (h *fakeHandle) Answer(s string) { h.answers = append(h.answers, s) }
func (h *fakeHandle) Cancel()         { h.cancelled = true }

// rig is a screen over fakes: no greetd, no compositor.
type rig struct {
	s          *screen
	convs      []*fakeHandle
	cmds       []func() []string
	envs       [][]string
	connectErr error
	focused    widget.Widget
	quit       int
	power      []string
}

func newRig(t *testing.T, edit func(*Init)) *rig {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "last-session")
	init := Init{
		Config: config.Defaults(),
		Sessions: []Session{
			{ID: "niri", Name: "Niri", Exec: []string{"niri-session"}},
			{ID: "sway", Name: "Sway", Exec: []string{"sway"}},
		},
		Users:      []User{{Name: "ada", DisplayName: "Ada", Home: "/home/ada"}, {Name: "bob", DisplayName: "Bob", Home: "/home/bob"}},
		StatePath:  state,
		SessionEnv: []string{"XDG_SESSION_TYPE=wayland"},
	}
	init.Config.Greeter.ShowUserList = true
	init.Config.Greeter.ShowPowerButtons = true
	if edit != nil {
		edit(&init)
	}
	r := &rig{}
	r.s = newScreen(init, credential.Fonts{Text: face, Clock: face}, styling.Default(), deps{
		connect: func(cmd func() []string, env []string) (auth.Conversation, error) {
			if r.connectErr != nil {
				return nil, r.connectErr
			}
			r.cmds = append(r.cmds, cmd)
			r.envs = append(r.envs, env)
			return nil, nil
		},
		spawn: func(_ auth.Conversation, username string, onEvent func(auth.Event)) authHandle {
			h := &fakeHandle{username: username, emit: onEvent}
			r.convs = append(r.convs, h)
			return h
		},
		invoke:     func(fn func()) { fn() },
		focus:      func(w widget.Widget) { r.focused = w },
		quit:       func() { r.quit++ },
		now:        func() time.Time { return time.Date(2026, 3, 5, 9, 7, 0, 0, time.UTC) },
		power:      func(verb string) { r.power = append(r.power, verb) },
		loadImage:  func(string) (image.Image, error) { return nil, errors.New("none") },
		loadAvatar: func(string) (image.Image, error) { return nil, errors.New("none") },
	})
	return r
}

func (r *rig) conv(t *testing.T) *fakeHandle {
	t.Helper()
	if len(r.convs) == 0 {
		t.Fatal("no conversation started")
	}
	return r.convs[len(r.convs)-1]
}

func prompt(kind auth.PromptKind, text string) auth.Event {
	return auth.Event{Kind: auth.EventPrompt, Prompt: auth.Prompt{Kind: kind, Text: text}}
}

func TestGreeterStartsRevealedAndFocused(t *testing.T) {
	r := newRig(t, nil)
	r.s.start()
	if !r.s.prompt.Reveal.Revealed() || r.s.prompt.Reveal.Transition() != widget.RevealFade {
		t.Error("the card does not fade in")
	}
	if r.focused != r.s.prompt.Username {
		t.Error("with nobody remembered the username takes focus")
	}
	if r.s.prompt.Username.Placeholder() != "Username" {
		t.Errorf("placeholder = %q, want the localized one", r.s.prompt.Username.Placeholder())
	}

	r = newRig(t, func(i *Init) { i.LastUser = "ada"; i.LastSession = "sway" })
	r.s.start()
	if r.s.prompt.Username.Text() != "ada" || r.focused != r.s.prompt.Entry {
		t.Error("a remembered user pre-fills and the password takes focus")
	}
	if r.s.selectedSession().ID != "sway" {
		t.Errorf("selected session = %q, want the remembered sway", r.s.selectedSession().ID)
	}
}

func TestGreeterLoginSucceeds(t *testing.T) {
	r := newRig(t, nil)
	r.s.prompt.Username.SetText(" ada ")
	r.s.submit("hunter2")
	h := r.conv(t)
	if h.username != "ada" || r.s.prompt.Username.Enabled() || r.s.prompt.Entry.Enabled() {
		t.Fatalf("a submit starts a conversation for the trimmed user with the form locked: %+v", h)
	}
	if !slices.Equal(r.envs[0], []string{"XDG_SESSION_TYPE=wayland"}) {
		t.Errorf("session env = %v", r.envs[0])
	}
	// The command is read when greetd starts the session: a change of
	// mind mid-login is what runs.
	r.s.drop.OnSelect(1)
	if got := r.cmds[0](); !slices.Equal(got, []string{"sway"}) {
		t.Errorf("session command = %v, want the picked sway", got)
	}
	h.emit(prompt(auth.PromptSecret, "Password:"))
	if !slices.Equal(h.answers, []string{"hunter2"}) {
		t.Fatalf("answers = %v, want the stashed password", h.answers)
	}
	h.emit(auth.Event{Kind: auth.EventSuccess})
	if r.quit != 1 {
		t.Error("success did not hand over to greetd")
	}
	if got := LoadLast(r.s.init.StatePath); got != "sway" {
		t.Errorf("remembered session = %q", got)
	}
	if got := LoadLast(LastUserPath(r.s.init.StatePath)); got != "ada" {
		t.Errorf("remembered user = %q", got)
	}
}

func TestGreeterSecondPromptAndInfo(t *testing.T) {
	r := newRig(t, nil)
	r.s.prompt.Username.SetText("ada")
	r.s.submit("pw")
	h := r.conv(t)
	h.emit(prompt(auth.PromptSecret, "Password:"))
	h.emit(prompt(auth.PromptInfo, "Checking"))
	if r.s.prompt.Error.Text() != "Checking" {
		t.Errorf("info = %q", r.s.prompt.Error.Text())
	}
	h.emit(prompt(auth.PromptVisible, "OTP:"))
	if !r.s.awaiting || !r.s.prompt.Entry.Enabled() || r.focused != r.s.prompt.Entry || r.s.prompt.Error.Text() != "OTP:" {
		t.Fatal("a second prompt re-enables the entry and waits")
	}
	r.s.submit("123456")
	if !slices.Equal(h.answers, []string{"pw", "123456"}) || r.s.prompt.Entry.Enabled() {
		t.Errorf("answers = %v; the entry locks again while greetd works", h.answers)
	}
	// A stray submit while greetd works is ignored.
	r.s.submit("again")
	if len(r.convs) != 1 || len(h.answers) != 2 {
		t.Error("a submit mid-conversation started or answered something")
	}
}

func TestGreeterFailureRearms(t *testing.T) {
	r := newRig(t, nil)
	r.s.prompt.Username.SetText("ada")
	r.s.submit("wrong")
	first := r.conv(t)
	first.emit(auth.Event{Kind: auth.EventFailure, Reason: "Authentication failed"})
	if r.s.prompt.Error.Text() != "Authentication failed" || !r.s.prompt.Entry.Enabled() || r.s.prompt.Username.Text() != "ada" {
		t.Fatal("a failure shows the reason, keeps the user and re-arms the form")
	}
	if r.quit != 0 {
		t.Error("a failure quit")
	}
	r.s.submit("right")
	if len(r.convs) != 2 {
		t.Fatal("the next submit did not start a fresh conversation")
	}
	// The dead conversation's late events change nothing.
	first.emit(auth.Event{Kind: auth.EventSuccess})
	if r.quit != 0 {
		t.Error("a stale conversation's success was taken")
	}
}

func TestGreeterRefusesAnEmptyUserAndANoGreetd(t *testing.T) {
	r := newRig(t, nil)
	r.s.submit("pw")
	if len(r.convs) != 0 || r.s.prompt.Error.Text() != "Enter a username" || r.focused != r.s.prompt.Username {
		t.Error("an empty username starts nothing and asks for one")
	}
	r.connectErr = errors.New("no GREETD_SOCK")
	r.s.prompt.Username.SetText("ada")
	r.s.submit("pw")
	if len(r.convs) != 0 || r.s.prompt.Error.Text() != "greetd unavailable: \u2068no GREETD_SOCK\u2069" || !r.s.prompt.Entry.Enabled() {
		t.Errorf("no greetd: message %q, entry enabled %v", r.s.prompt.Error.Text(), r.s.prompt.Entry.Enabled())
	}
}

func TestGreeterUserRowAndPower(t *testing.T) {
	r := newRig(t, nil)
	if len(r.s.users) != 2 {
		t.Fatalf("user buttons = %d", len(r.s.users))
	}
	r.s.users[1].button.OnClick()
	if r.s.prompt.Username.Text() != "bob" || r.focused != r.s.prompt.Entry || r.s.users[1].button.Bg != userSelected || r.s.users[0].button.Bg != 0 {
		t.Error("a user click fills the name, marks it and focuses the password")
	}
	many := make([]User, maxListedUsers+1)
	for i := range many {
		many[i] = User{Name: "u", DisplayName: "U"}
	}
	if r := newRig(t, func(i *Init) { i.Users = many }); len(r.s.users) != 0 {
		t.Error("more users than the cap still listed")
	}
	if r := newRig(t, func(i *Init) { i.Config.Greeter.ShowUserList = false }); len(r.s.users) != 0 {
		t.Error("show-user-list off still listed users")
	}
	// The power buttons call systemctl's verbs.
	var buttons []*widget.Button
	var walk func(widget.Widget)
	walk = func(w widget.Widget) {
		if b, ok := w.(*widget.Button); ok && b.TooltipText() != "" {
			buttons = append(buttons, b)
		}
		if c, ok := w.(interface{ Children() []widget.Widget }); ok {
			for _, k := range c.Children() {
				walk(k)
			}
		}
	}
	walk(r.s.root)
	for _, b := range buttons {
		b.OnClick()
	}
	if !slices.Equal(r.power, []string{"poweroff", "reboot"}) {
		t.Errorf("power verbs = %v", r.power)
	}
}

func TestGreeterSessionPickerAndCaps(t *testing.T) {
	if r := newRig(t, func(i *Init) { i.Sessions = i.Sessions[:1] }); r.s.drop.Visible() {
		t.Error("a single session still shows the picker")
	}
	r := newRig(t, nil)
	if !r.s.drop.Visible() {
		t.Error("two sessions hide the picker")
	}
	r.s.setCapsLock(true)
	if !r.s.caps.Visible() || r.s.caps.Text() != "Caps Lock is on" {
		t.Error("caps lock on shows the warning")
	}
	r.s.setCapsLock(false)
	if r.s.caps.Visible() {
		t.Error("caps lock off keeps the warning")
	}
}

func TestCustomSessionName(t *testing.T) {
	if CustomSessionName() != "Custom" {
		t.Errorf("custom = %q", CustomSessionName())
	}
}
