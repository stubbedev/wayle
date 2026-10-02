package settings

import (
	"fmt"
	"log"
	"time"

	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/shell/apptheme"
)

// appID is the window's application id (com.wayle.settings, the
// desktop entry's name).
const appID = "com.wayle.settings"

// Run loads the config and shows the settings window until it closes
// (main.rs). A config that cannot load is an error: the window edits
// it, so there is nothing to show without it.
func Run() error {
	svc, err := config.Open()
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}
	defer svc.Close()
	sess, err := app.Connect()
	if err != nil {
		return fmt.Errorf("settings: connect: %w", err)
	}
	defer sess.Close()

	application := app.NewApplication(sess)
	defer apptheme.Setup()()
	cfg := svc.Config()
	theme := apptheme.New(cfg)
	theme.WatchUserStyles(application)
	face, err := app.Font(cfg.General.FontSans, 14)
	if err != nil {
		return fmt.Errorf("settings: font %q: %w", cfg.General.FontSans, err)
	}
	k := &kit{face: app.FontFallback(face), store: store{svc}, invoke: application.Invoke}
	after := func(d time.Duration, fn func()) {
		time.AfterFunc(d, func() { application.Invoke(fn) })
	}
	w := newWindow(k, cfg, after)
	theme.Attach(w.root)

	win, err := application.NewWindow(app.WindowConfig{
		Title: i18n.Settings().Get("settings-title"), AppID: appID,
		Width: windowWidth, Height: windowHeight,
		Root: w.root, Background: theme.RenderPalette().Surface,
	})
	if err != nil {
		return fmt.Errorf("settings: window: %w", err)
	}
	w.onClose = win.Close
	w.onResetAll = func() { confirmResetAll(application, win, k, theme) }

	// A change from any source - a row, the reset buttons, the config
	// file, another wayle - restyles the window and refreshes the page.
	cancel := svc.Subscribe(func(_, next *config.Config) {
		application.Invoke(func() {
			theme.SetConfig(next)
			w.refresh()
		})
	})
	defer cancel()
	return application.Run()
}

// confirmResetAll asks, then drops every runtime override
// (ConfirmResetAll, ExecuteResetAll).
func confirmResetAll(application *app.Application, parent *app.Window, k *kit, theme *apptheme.Theme) {
	var d *app.Dialog
	respond := func(r string) {
		if d != nil {
			d.Respond(r)
		}
	}
	content := confirmContent(k, resetAllConfirm(), respond)
	theme.Attach(content)
	d, err := application.NewDialog(parent, app.DialogConfig{
		Title: resetAllConfirm().title, Content: content, Bare: true, Background: theme.RenderPalette().Surface,
		CancelResponse: responseCancel,
		OnResponse: func(r string) {
			if r == responseConfirm {
				k.store.resetAll()
			}
		},
	})
	if err != nil {
		log.Printf("settings: reset-all dialog: %v", err)
	}
}
