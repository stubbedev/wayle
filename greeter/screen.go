package greeter

import (
	_ "embed" // the power-button glyphs
	"fmt"
	"image"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/auth"
	"github.com/stubbedev/wayle/shell/credential"
)

// t is the greeter's t! (wayle-greeter.ftl, the desktop locale).
func t(id string, args ...i18n.Arg) string { return i18n.Greeter().Get(id, args...) }

// CustomSessionName labels the explicit `-- <argv>` session.
func CustomSessionName() string { return t("greeter-custom-session") }

// revealFade is the credential card's reveal, fixed in the greeter
// (CredentialOpts: Fade, 300ms) rather than [animations].
const revealFade = 300 * time.Millisecond

// maxListedUsers caps the avatar row; beyond it the username entry
// carries the load.
const maxListedUsers = 8

// The power glyphs, embedded: pre-login there is no icon theme to rely
// on.
var (
	//go:embed assets/ld-power-symbolic.svg
	powerSVG []byte
	//go:embed assets/ld-rotate-ccw-symbolic.svg
	restartSVG []byte
)

// The greeter's fixed metrics: GTK widget properties, not styling.
const (
	avatarPx    = 64 // the avatar's size request
	userRowGap  = 12 // the user row's spacing
	userColGap  = 6  // the avatar column's spacing
	powerGap    = 12 // the power row's spacing
	powerIconPx = 16
	nameChars   = 14 // the user name's max-width-chars
	textPx      = 16 // the labels' size before the stylesheet's
)

// Init is what the greeter starts from (app.rs GreeterInit).
type Init struct {
	Config *config.Config
	// Sessions is non-empty (main enforces it).
	Sessions    []Session
	Users       []User
	LastSession string
	LastUser    string
	StatePath   string
	SessionEnv  []string
}

// deps are the screen's collaborators; Run fills the production ones.
type deps struct {
	// connect opens the greetd conversation for this attempt; cmd
	// resolves the selected session at start_session.
	connect func(cmd func() []string, env []string) (auth.Conversation, error)
	spawn   func(conv auth.Conversation, username string, onEvent func(auth.Event)) authHandle
	invoke  func(func())
	focus   func(widget.Widget)
	quit    func()
	now     func() time.Time
	power   func(verb string)
	// loadImage decodes the background (no blur in the greeter).
	loadImage func(path string) (image.Image, error)
	// loadAvatar decodes an avatar file.
	loadAvatar func(path string) (image.Image, error)
}

type authHandle interface {
	Answer(string)
	Cancel()
}

// screen is the greeter UI and its login state machine.
type screen struct {
	init   Init
	d      deps
	prompt *credential.Prompt
	root   widget.Widget
	drop   *widget.Dropdown
	caps   *widget.Label
	users  []*userButton

	mu       sync.Mutex // guards selected: greetd reads it off the loop
	selected int

	auth     authHandle
	authGen  uint64
	awaiting bool
	pending  *string
}

// userButton is one avatar in the user list.
type userButton struct {
	user   User
	button *widget.Button
}

// newScreen builds the greeter view (app.rs init): font shapes the
// text and sheet styles the window through the _lock.scss rules the
// greeter shares with the lock screen.
func newScreen(init Init, font render.Font, sheet *widget.Stylesheet, d deps) *screen {
	s := &screen{init: init, d: d}
	g := init.Config.Greeter

	names := make([]string, len(init.Sessions))
	for i, sess := range init.Sessions {
		names[i] = sess.Name
		if sess.ID == init.LastSession && init.LastSession != "" {
			s.selected = i
		}
	}
	s.drop = widget.NewDropdown(font, textPx, names, s.selected)
	s.drop.AddClass("lock-session")
	s.drop.SetCursorName("pointer")
	s.drop.SetHAlign(widget.AlignCenter)
	s.drop.OnSelect = func(i int) {
		s.mu.Lock()
		s.selected = i
		s.mu.Unlock()
	}
	s.drop.SetVisible(len(init.Sessions) > 1)
	s.caps = widget.NewLabel(font, textPx, t("greeter-caps-lock"), 0)
	s.caps.AddClass("lock-caps")
	s.caps.SetVisible(false)
	below := widget.NewBox(widget.Column, 0, 0)
	below.Append(s.caps, false).Append(s.drop, false)

	users := widget.NewBox(widget.Row, userRowGap, 0)
	users.AddClass("lock-users")
	users.SetHAlign(widget.AlignCenter)
	if g.ShowUserList && len(init.Users) > 0 && len(init.Users) <= maxListedUsers {
		s.populateUsers(users, font)
	} else {
		users.SetVisible(false)
	}
	s.prompt = credential.Build(credential.Options{
		Font: font, ShowClock: g.ShowClock, WithUsername: true,
		Header: users, Extra: below, Focus: d.focus,
	}, s.submit)
	s.prompt.Username.SetPlaceholder(t("greeter-username"))
	s.prompt.Reveal.SetTransition(widget.RevealFade)
	s.prompt.Reveal.SetDuration(revealFade)

	layers := []widget.Widget{
		credential.Background(s.background(), credential.HexFill(g.Background.Color)),
		s.prompt.Reveal,
	}
	if g.ShowPowerButtons {
		layers = append(layers, s.powerRow())
	}
	s.root = credential.Window(sheet, layers...)
	s.refreshClock()
	return s
}

// start pre-fills the remembered user and focuses the next field:
// the password when the username is known, the username otherwise.
func (s *screen) start() {
	s.prompt.Reveal.SetRevealed(true)
	if s.init.LastUser != "" {
		s.prompt.Username.SetText(s.init.LastUser)
		s.d.focus(s.prompt.Entry)
		return
	}
	s.d.focus(s.prompt.Username)
}

// background is the greeter's background (app.rs build_background): an
// unreadable image falls back to the color, never a mystery black.
func (s *screen) background() image.Image {
	g := s.init.Config.Greeter
	path := g.Background.ImagePath(s.init.Config.Wallpaper.Wallpaper)
	if path == "" {
		return nil
	}
	img, err := s.d.loadImage(path)
	if err != nil {
		log.Printf("greeter: background image unreadable; using color fill: %v", err)
		return nil
	}
	return img
}

// populateUsers is populate_user_row: one avatar button per login user
// in the .lock-users row; a click fills the username, marks the button
// selected, and focuses the password.
func (s *screen) populateUsers(row *widget.Box, font render.Font) {
	for _, u := range s.init.Users {
		ub := &userButton{user: u}
		name := widget.NewLabel(font, textPx, u.DisplayName, 0)
		name.AddClass("lock-user-name")
		name.SetEllipsize(widget.EllipsizeEnd)
		name.SetMaxWidthChars(nameChars)
		col := widget.NewBox(widget.Column, userColGap, 0)
		col.SetHAlign(widget.AlignCenter)
		col.Append(s.avatar(u, font), false).Append(name, false)
		ub.button = widget.NewButton(col, 0, 0)
		ub.button.AddClass("lock-user-button", "flat")
		if u.Name == s.init.LastUser {
			ub.button.AddClass("selected")
		}
		ub.button.OnClick = func() { s.pickUser(ub) }
		s.users = append(s.users, ub)
		row.Append(ub.button, false)
	}
}

func (s *screen) pickUser(picked *userButton) {
	for _, ub := range s.users {
		ub.button.RemoveClass("selected")
	}
	picked.button.AddClass("selected")
	s.prompt.Username.SetText(picked.user.Name)
	s.d.focus(s.prompt.Entry)
}

// avatar is build_user_button's avatar: the image scaled to cover, or
// the initial letter (.lock-avatar-fallback) when there is no readable
// image, at the 64px size request.
func (s *screen) avatar(u User, font render.Font) widget.Widget {
	sized := fmt.Sprintf("min-width: %dpx; min-height: %dpx;", avatarPx, avatarPx)
	if u.Icon != "" {
		if img, err := s.d.loadAvatar(u.Icon); err == nil {
			pic := widget.NewImage(img)
			pic.SetScale(widget.ImageCover)
			pic.AddClass("lock-avatar")
			pic.SetInlineStyle(sized)
			return pic
		}
	}
	initial := ""
	for _, r := range u.DisplayName {
		initial = string(unicode.ToUpper(r))
		break
	}
	letter := widget.NewLabel(font, textPx, initial, 0)
	letter.AddClass("lock-avatar", "lock-avatar-fallback")
	letter.SetAlignment(render.AlignCenter)
	letter.SetInlineStyle(sized)
	return letter
}

// powerRow is build_power_box: shutdown and reboot in the .lock-power
// row at the bottom center.
func (s *screen) powerRow() widget.Widget {
	row := widget.NewBox(widget.Row, powerGap, 0)
	row.AddClass("lock-power")
	row.SetHAlign(widget.AlignCenter)
	row.SetVAlign(widget.AlignEnd)
	for _, b := range []struct {
		svg     []byte
		tooltip string
		verb    string
	}{{powerSVG, t("greeter-shutdown"), "poweroff"}, {restartSVG, t("greeter-restart"), "reboot"}} {
		btn := widget.NewButton(widget.NewSVGIcon(b.svg, powerIconPx), 0, 0)
		btn.AddClass("lock-power-button", "flat")
		btn.SetTooltip(b.tooltip)
		verb := b.verb
		btn.OnClick = func() { s.d.power(verb) }
		row.Append(btn, false)
	}
	return row
}

// selectedSession is the session the picker shows.
func (s *screen) selectedSession() Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.init.Sessions[s.selected]
}

// submit is Enter in the password entry: answer a prompt on screen, or
// start a login with the entered username.
func (s *screen) submit(value string) {
	if s.awaiting {
		s.awaiting = false
		if s.auth != nil {
			s.auth.Answer(value)
		}
		s.prompt.Entry.SetText("")
		s.prompt.Entry.SetEnabled(false)
		return
	}
	if s.auth != nil {
		return
	}
	username := strings.TrimSpace(s.prompt.Username.Text())
	if username == "" {
		s.prompt.SetMessage(t("greeter-enter-username"))
		s.d.focus(s.prompt.Username)
		return
	}
	s.prompt.SetMessage("")
	s.setForm(false)
	s.startConversation(username, value)
}

func (s *screen) startConversation(username, password string) {
	conv, err := s.d.connect(func() []string { return s.selectedSession().Exec }, s.init.SessionEnv)
	if err != nil {
		log.Printf("greeter: cannot connect to greetd: %v", err)
		s.prompt.SetMessage(t("greeter-greetd-unavailable", i18n.Str("error", err.Error())))
		s.setForm(true)
		return
	}
	s.pending = &password
	s.authGen++
	gen := s.authGen
	s.auth = s.d.spawn(conv, username, func(ev auth.Event) {
		s.d.invoke(func() {
			if gen == s.authGen {
				s.onEvent(ev)
			}
		})
	})
}

func (s *screen) onEvent(ev auth.Event) {
	switch ev.Kind {
	case auth.EventPrompt:
		s.onPrompt(ev.Prompt)
	case auth.EventSuccess:
		log.Printf("greeter: authentication succeeded; greetd is starting the session")
		s.rememberLogin()
		s.d.quit()
	case auth.EventFailure:
		log.Printf("greeter: authentication failed: %s", ev.Reason)
		s.auth = nil
		s.authGen++
		s.awaiting = false
		s.pending = nil
		s.prompt.SetMessage(ev.Reason)
		// Keep the username, clear the password; the next submit starts
		// a fresh conversation (greetd cancelled this one).
		s.setForm(true)
		s.prompt.Entry.SetText("")
		s.d.focus(s.prompt.Entry)
	}
}

// onPrompt: the stashed password answers the first input prompt;
// later ones (OTP, expired password) re-enable the entry and wait for
// the next submit. Info and error prompts show their text.
func (s *screen) onPrompt(p auth.Prompt) {
	if !p.WantsInput() {
		s.prompt.SetMessage(p.Text)
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
	s.prompt.SetMessage(p.Text)
	s.prompt.Entry.SetEnabled(true)
	s.prompt.Entry.SetText("")
	s.d.focus(s.prompt.Entry)
}

// setForm enables or disables the whole login form.
func (s *screen) setForm(on bool) {
	s.prompt.Username.SetEnabled(on)
	s.prompt.Entry.SetEnabled(on)
	s.drop.SetEnabled(on)
}

// rememberLogin persists the session and the username for next time.
func (s *screen) rememberLogin() {
	if err := SaveLast(s.init.StatePath, s.selectedSession().ID); err != nil {
		log.Printf("greeter: could not save last session: %v", err)
	}
	if user := strings.TrimSpace(s.prompt.Username.Text()); user != "" {
		if err := SaveLast(LastUserPath(s.init.StatePath), user); err != nil {
			log.Printf("greeter: could not save last user: %v", err)
		}
	}
}

func (s *screen) refreshClock() {
	now := s.d.now()
	g := s.init.Config.Greeter
	s.prompt.Clock.SetText(g.ClockFormat.Layout().Format(now))
	s.prompt.Date.SetText(g.DateFormat.Layout().Format(now))
}

// setCapsLock shows or hides the warning.
func (s *screen) setCapsLock(on bool) {
	if s.caps.Visible() != on {
		s.caps.SetVisible(on)
	}
}

// decodeImage reads any image the credential loader supports.
func decodeImage(path string) (image.Image, error) { return credential.LoadImage(path, 0) }
