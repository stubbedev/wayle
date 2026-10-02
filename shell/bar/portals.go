package bar

import (
	"log"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/desktopentry"
	"github.com/stubbedev/wayle/shell/portaldialogs"
)

// servePortalDialogs hosts com.wayle.PortalDialogs1, the native dialogs
// behind the portal backend's Access, Account, AppChooser,
// DynamicLauncher and wallpaper preview (services/portal_dialogs). A
// bus failure leaves the portal answering those requests "other".
func servePortalDialogs(application *app.Application, current func() *config.Config, font render.Font, ink render.Color, sheet *widget.Stylesheet) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("portal dialogs: session bus: %v", err)
		return
	}
	host := portaldialogs.New(portaldialogs.Deps{
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
	if _, err := portaldialogs.NewDaemon(host).Export(conn); err != nil {
		log.Printf("portal dialogs: daemon: %v", err)
		_ = conn.Close()
	}
}
