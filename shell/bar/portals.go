package bar

import (
	"context"
	"log"
	"os"
	"os/user"
	"path/filepath"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/desktopentry"
	"github.com/stubbedev/wayle/internal/ipp"
	"github.com/stubbedev/wayle/internal/mime"
	"github.com/stubbedev/wayle/internal/xdg"
	"github.com/stubbedev/wayle/shell/filechooser"
	"github.com/stubbedev/wayle/shell/portaldialogs"
	"github.com/stubbedev/wayle/shell/printdialog"
)

// servePortalHosts hosts the shell's halves of the portal backend on
// one session bus connection: com.wayle.PortalDialogs1 (access,
// account, app chooser, dynamic launcher and the wallpaper preview;
// services/portal_dialogs), com.wayle.FileChooser1 (services/
// file_chooser) and com.wayle.Print1 (services/print, spooling over
// CUPS' IPP). A bus failure leaves the portal answering those requests
// "other".
func servePortalHosts(application *app.Application, current func() *config.Config, font render.Font, ink render.Color, sheet *widget.Stylesheet) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("portal hosts: session bus: %v", err)
		return
	}
	dialogs := portaldialogs.New(portaldialogs.Deps{
		Config:     current,
		Open:       layerOpener[portaldialogs.Window](application),
		Invoke:     application.Invoke,
		Font:       font,
		Ink:        ink,
		Sheet:      sheet,
		Candidates: portaldialogs.CandidateApps,
		SetDefault: desktopentry.SetDefault,
		Avatar:     portaldialogs.LocalAvatar,
	})
	exported := 0
	if _, err := portaldialogs.NewDaemon(dialogs).Export(conn); err != nil {
		log.Printf("portal dialogs: daemon: %v", err)
	} else {
		exported++
	}

	home, _ := os.UserHomeDir()
	viewFile := ""
	if dir, ok := xdg.StateDir(); ok {
		viewFile = filepath.Join(dir, "file-chooser-view")
	}
	chooser := filechooser.New(filechooser.Deps{
		Config:    current,
		Open:      layerOpener[filechooser.Window](application),
		Invoke:    application.Invoke,
		Font:      font,
		Ink:       ink,
		Sheet:     sheet,
		MIME:      mime.System(),
		Home:      home,
		Places:    func() []filechooser.Place { return filechooser.UserPlaces(home) },
		Locations: filechooser.SystemLocations(home),
		ViewFile:  viewFile,
	})
	if _, err := filechooser.NewDaemon(chooser).Export(conn); err != nil {
		log.Printf("file chooser: daemon: %v", err)
	} else {
		exported++
	}
	cups := ipp.NewClient(ipp.DefaultServer())
	printer := printdialog.New(printdialog.Deps{
		Config:     current,
		Open:       layerOpener[printdialog.Window](application),
		Invoke:     application.Invoke,
		Font:       font,
		Ink:        ink,
		Sheet:      sheet,
		Printers:   cups.Printers,
		Spool:      func(ctx context.Context, j ipp.Job) error { _, err := cups.Print(ctx, j); return err },
		OutputFile: printdialog.FileOutput,
		User:       currentUser(),
	})
	if _, err := printdialog.NewDaemon(printer).Export(conn); err != nil {
		log.Printf("print: daemon: %v", err)
	} else {
		exported++
	}
	if exported == 0 {
		_ = conn.Close()
	}
}

// currentUser names print jobs' owner: $USER, else the account.
func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// layerOpener maps a host's overlay as its own Window type; a failure
// is a nil interface, never a typed nil.
func layerOpener[W interface{ Close() }](application *app.Application) func(app.LayerConfig) (W, error) {
	return func(cfg app.LayerConfig) (W, error) {
		w, err := application.NewLayer(cfg)
		if err != nil {
			var none W
			return none, err
		}
		// Always holds: the assertions below pin every host's Window.
		win, _ := any(w).(W)
		return win, nil
	}
}

// Every host's Window is a layer window.
var (
	_ portaldialogs.Window = (*app.LayerWindow)(nil)
	_ filechooser.Window   = (*app.LayerWindow)(nil)
	_ printdialog.Window   = (*app.LayerWindow)(nil)
)
