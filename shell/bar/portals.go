package bar

import (
	"log"
	"os"
	"path/filepath"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/desktopentry"
	"github.com/stubbedev/wayle/internal/mime"
	"github.com/stubbedev/wayle/internal/xdg"
	"github.com/stubbedev/wayle/shell/filechooser"
	"github.com/stubbedev/wayle/shell/portaldialogs"
)

// servePortalHosts hosts the shell's halves of the portal backend on
// one session bus connection: com.wayle.PortalDialogs1, the native
// dialogs behind Access, Account, AppChooser, DynamicLauncher and the
// wallpaper preview (services/portal_dialogs), and
// com.wayle.FileChooser1, the file dialog (services/file_chooser). A
// bus failure leaves the portal answering those requests "other".
func servePortalHosts(application *app.Application, current func() *config.Config, font render.Font, ink render.Color, sheet *widget.Stylesheet) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("portal hosts: session bus: %v", err)
		return
	}
	dialogs := portaldialogs.New(portaldialogs.Deps{
		Config: current,
		Open: func(cfg app.LayerConfig) (portaldialogs.Window, error) {
			w, err := application.NewLayer(cfg)
			if err != nil {
				return nil, err
			}
			return w, nil
		},
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
		Config: current,
		Open: func(cfg app.LayerConfig) (filechooser.Window, error) {
			w, err := application.NewLayer(cfg)
			if err != nil {
				return nil, err
			}
			return w, nil
		},
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
	if exported == 0 {
		_ = conn.Close()
	}
}
