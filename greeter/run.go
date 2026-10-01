package greeter

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/auth"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/styling"
)

// Run shows the greeter until greetd starts a session (or the window
// closes): one fullscreen window on the kiosk compositor.
func Run(init Init) error {
	// gelm draws the cursor client-side from the XCURSOR_* theme: set
	// the resolved one before connecting.
	ApplyCursor(ResolveCursor(init.Config.Greeter, DetectCursor(cursorHome(init.Users, init.LastUser), init.LastSession)))

	sess, err := app.Connect()
	if err != nil {
		return fmt.Errorf("greeter: connect: %w", err)
	}
	defer sess.Close()
	a := app.NewApplication(sess)

	family := init.Config.General.FontSans
	base, err := app.Font(family, 16)
	if err != nil {
		return fmt.Errorf("greeter: font %q: %w", family, err)
	}
	fonts := credential.Fonts{Text: app.FontFallback(base), Clock: app.FontFallback(base)}
	if bold, err := app.FontWeighted(family, 64, 700, false); err == nil {
		fonts.Clock = app.FontFallback(bold)
	}

	var win *app.Window
	// The theme the desktop and the lock use (install_css: the configured
	// palette).
	pal, err := styling.ConfigPalette(init.Config.Styling)
	if err != nil {
		log.Printf("greeter: palette: %v", err)
	}
	s := newScreen(init, fonts, pal, deps{
		connect: func(cmd func() []string, env []string) (auth.Conversation, error) {
			return auth.NewGreetdFromEnv(cmd, env)
		},
		spawn: func(conv auth.Conversation, username string, onEvent func(auth.Event)) authHandle {
			return auth.Spawn(conv, username, onEvent)
		},
		invoke: a.Invoke,
		focus: func(w widget.Widget) {
			if win != nil {
				win.SetFocus(w)
			}
		},
		quit: a.Quit,
		now:  time.Now,
		power: func(verb string) {
			// logind allows both for the active local session without
			// extra polkit rules.
			if err := exec.Command("systemctl", verb).Start(); err != nil { //nolint:gosec // a fixed verb
				log.Printf("greeter: power action %s failed: %v", verb, err)
			}
		},
		loadImage:  decodeImage,
		loadAvatar: decodeImage,
	})
	win, err = a.NewWindow(app.WindowConfig{
		Title:      "wayle-greeter",
		AppID:      "dev.stubbe.wayle.greeter",
		Root:       s.root,
		Background: render.RGB(0, 0, 0),
	})
	if err != nil {
		return err
	}
	win.Fullscreen()
	s.start()
	a.Every(time.Second, s.refreshClock)
	a.Every(250*time.Millisecond, func() { s.setCapsLock(sess.Mods()&app.ModCapsLock != 0) })
	s.applyDebugOps(os.Getenv("WAYLE_GREETER_DEBUG"), func(d time.Duration, fn func()) {
		time.AfterFunc(d, func() { a.Invoke(fn) })
	})
	log.Printf("wayle-greeter starting (%d sessions)", len(init.Sessions))
	err = a.Run()
	if err == app.ErrClosed { //nolint:errorlint // the loop's sentinel
		return nil
	}
	return err
}

// cursorHome is whose dotfiles seed cursor detection: the remembered
// user's, else the only user's.
func cursorHome(users []User, lastUser string) string {
	for _, u := range users {
		if u.Name == lastUser && lastUser != "" {
			return u.Home
		}
	}
	if len(users) == 1 {
		return users[0].Home
	}
	return ""
}

// applyDebugOps is the dev-harness hook (WAYLE_GREETER_DEBUG):
// comma-separated "popup" opens the session picker and
// "login=<user>:<pass>" fills the form and submits, each a second in.
// greetd never forwards the variable, so production never sees it.
func (s *screen) applyDebugOps(spec string, after func(time.Duration, func())) {
	if spec == "" {
		return
	}
	for op := range strings.SplitSeq(spec, ",") {
		if op == "popup" {
			after(time.Second, s.drop.Open)
			continue
		}
		login, ok := strings.CutPrefix(op, "login=")
		if !ok {
			continue
		}
		user, pass, ok := strings.Cut(login, ":")
		if !ok {
			continue
		}
		after(time.Second, func() {
			s.prompt.Username.SetText(user)
			s.submit(pass)
		})
	}
}
