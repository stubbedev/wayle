package greeter

import (
	_ "embed" // the power-button glyphs
	"image"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/auth"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/styling"
)

// The greeter's strings (wayle-greeter.ftl, en-US).
const (
	msgEnterUsername = "Enter a username"
	msgCapsLock      = "Caps Lock is on"
	msgShutdown      = "Shut down"
	msgRestart       = "Restart"
	// CustomSessionName labels the explicit `-- <argv>` session.
	CustomSessionName = "Custom"
)

func msgGreetdUnavailable(err error) string { return "greetd unavailable: " + err.Error() }

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

// The _lock.scss greeter rules.
var (
	userHover    = render.RGBA(0, 0, 0, 89)  // .lock-user-button:hover
	userSelected = render.RGBA(0, 0, 0, 128) // .lock-user-button.selected
	avatarFill   = render.RGBA(0, 0, 0, 115) // .lock-avatar-fallback
	powerHover   = render.RGBA(0, 0, 0, 89)  // .lock-power-button:hover
)

const (
	avatarPx     = 64
	userRowGap   = 12
	powerMargin  = 32 // .lock-power margin-bottom: 2rem
	powerIconPx  = 16
	capsPx       = 14.4 // .lock-caps font-size: 0.9rem
	userNamePx   = 14.4 // .lock-user-name font-size: 0.9rem
	avatarTextPx = 28   // .lock-avatar-fallback font-size: 1.75rem
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

func newScreen(init Init, fonts credential.Fonts, pal *styling.Palette, d deps) *screen {
	s := &screen{init: init, d: d}
	g := init.Config.Greeter

	names := make([]string, len(init.Sessions))
	for i, sess := range init.Sessions {
		names[i] = sess.Name
		if sess.ID == init.LastSession && init.LastSession != "" {
			s.selected = i
		}
	}
	s.drop = widget.NewDropdown(fonts.Text, 16, names, s.selected)
	s.drop.OnSelect = func(i int) {
		s.mu.Lock()
		s.selected = i
		s.mu.Unlock()
	}
	s.drop.SetVisible(len(init.Sessions) > 1)
	s.caps = widget.NewLabel(fonts.Text, capsPx, msgCapsLock, pal.Yellow)
	s.caps.SetAlignment(render.AlignCenter)
	s.caps.SetVisible(false)
	below := widget.NewBox(widget.Column, 0, 0)
	below.Append(s.caps, false).Append(credential.NewSpacer(0, 16), false).Append(credential.Center(s.drop), false)

	var header widget.Widget
	if g.ShowUserList && len(init.Users) > 0 && len(init.Users) <= maxListedUsers {
		header = credential.Center(s.userRow(fonts, pal))
	}
	s.prompt = credential.Build(credential.Options{
		Fonts: fonts, Palette: pal, ShowClock: g.Clock.Show, WithUsername: true,
		Header: header, Extra: below, Focus: d.focus,
	}, s.submit)

	layers := widget.NewOverlay().
		Append(credential.Background(s.background(), credential.HexFill(g.Background.Color))).
		Append(s.prompt.Root)
	if g.ShowPowerButtons {
		layers.Append(s.powerRow(pal))
	}
	s.root = layers
	s.refreshClock()
	return s
}

// start pre-fills the remembered user and focuses the next field:
// the password when the username is known, the username otherwise.
func (s *screen) start() {
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

// userRow is one avatar button per login user; a click fills the
// username, marks the button selected, and focuses the password.
func (s *screen) userRow(fonts credential.Fonts, pal *styling.Palette) widget.Widget {
	row := widget.NewBox(widget.Row, userRowGap, 0)
	for _, u := range s.init.Users {
		ub := &userButton{user: u}
		name := widget.NewLabel(fonts.Text, userNamePx, u.DisplayName, pal.FgMuted)
		name.SetAlignment(render.AlignCenter)
		name.SetEllipsize(widget.EllipsizeEnd)
		col := widget.NewBox(widget.Column, 6, 0)
		col.Append(credential.Center(s.avatar(u, fonts, pal)), false).Append(credential.NewFixed(name, 14*9, 0), false)
		ub.button = widget.NewButton(col, 8, 12)
		ub.button.BgExplicit = true
		ub.button.BgHover = userHover
		ub.button.BgPressed = userSelected
		if u.Name == s.init.LastUser {
			ub.button.Bg = userSelected
		}
		ub.button.OnClick = func() { s.pickUser(ub) }
		s.users = append(s.users, ub)
		row.Append(ub.button, false)
	}
	return row
}

func (s *screen) pickUser(picked *userButton) {
	for _, ub := range s.users {
		ub.button.Bg = 0
		ub.button.Invalidate()
	}
	picked.button.Bg = userSelected
	s.prompt.Username.SetText(picked.user.Name)
	s.d.focus(s.prompt.Entry)
}

// avatar is the round avatar image, or the initial letter on a dark
// disc when there is no readable image.
func (s *screen) avatar(u User, fonts credential.Fonts, pal *styling.Palette) widget.Widget {
	if u.Icon != "" {
		if img, err := s.d.loadAvatar(u.Icon); err == nil {
			return credential.NewFixed(widget.NewImage(roundAvatar(img, avatarPx)), avatarPx, avatarPx)
		}
	}
	initial := ""
	for _, r := range u.DisplayName {
		initial = string(unicode.ToUpper(r))
		break
	}
	letter := widget.NewLabel(fonts.Clock, avatarTextPx, initial, pal.Fg)
	letter.SetAlignment(render.AlignCenter)
	return credential.NewFixed(credential.NewPanel(credential.Center(letter), 0, avatarPx/2, avatarFill), avatarPx, avatarPx)
}

// roundAvatar scales img to cover a size x size square and masks it to
// a disc (.lock-avatar border-radius: 50%).
func roundAvatar(img image.Image, size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	if side == 0 {
		return out
	}
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			if dx*dx+dy*dy > r*r {
				continue
			}
			c := img.At(x0+x*side/size, y0+y*side/size)
			out.Set(x, y, c)
		}
	}
	return out
}

// powerRow is shutdown and reboot at the bottom center.
func (s *screen) powerRow(pal *styling.Palette) widget.Widget {
	row := widget.NewBox(widget.Row, 12, 0)
	for _, b := range []struct {
		svg     []byte
		tooltip string
		verb    string
	}{{powerSVG, msgShutdown, "poweroff"}, {restartSVG, msgRestart, "reboot"}} {
		icon := widget.NewSVGIcon(b.svg, powerIconPx)
		icon.SetTint(pal.FgMuted)
		btn := widget.NewButton(icon, 12, 12)
		btn.BgExplicit = true
		btn.BgHover = powerHover
		btn.SetTooltip(b.tooltip)
		verb := b.verb
		btn.OnClick = func() { s.d.power(verb) }
		row.Append(btn, false)
	}
	g := widget.NewGrid(0, 0)
	col := widget.NewBox(widget.Column, 0, 0)
	col.Append(row, false).Append(credential.NewSpacer(0, powerMargin), false)
	g.Attach(col, 0, 0, 1, 1)
	g.SetAlign(col, widget.AlignCenter, widget.AlignEnd)
	return g
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
		s.prompt.SetMessage(msgEnterUsername)
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
		s.prompt.SetMessage(msgGreetdUnavailable(err))
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
	c := s.init.Config.Greeter.Clock
	s.prompt.Clock.SetText(c.Time.Format(now))
	s.prompt.Date.SetText(c.Date.Format(now))
}

// setCapsLock shows or hides the warning.
func (s *screen) setCapsLock(on bool) {
	if s.caps.Visible() != on {
		s.caps.SetVisible(on)
	}
}

// decodeImage reads any image the credential loader supports.
func decodeImage(path string) (image.Image, error) { return credential.LoadImage(path, 0) }
