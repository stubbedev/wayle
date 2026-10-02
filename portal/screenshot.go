package portal

import (
	"context"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/internal/fileuri"
	"github.com/stubbedev/wayle/shell/screenshot"
)

// screenshotPortal bridges org.freedesktop.impl.portal.Screenshot to the
// shell's com.wayle.Screenshot1, the service `wayle screenshot` uses
// (screenshot.rs): an interactive request selects a region, any other
// grabs the whole multi-monitor screen as xdph's grim does.
type screenshotPortal struct{ conn *dbus.Conn }

func screenshotIface(conn *dbus.Conn) dbusx.Interface {
	return dbusx.Interface{
		Name:       "org.freedesktop.impl.portal.Screenshot",
		Methods:    screenshotPortal{conn},
		Properties: dbusx.Getters{"version": func() any { return uint32(2) }},
	}
}

// Screenshot captures and replies the saved file's URI; an empty path
// from the shell is the user cancelling the selection.
func (s screenshotPortal) Screenshot(_ dbus.ObjectPath, _, _ string, options Vardict) (uint32, Vardict, *dbus.Error) {
	mode := "screen"
	if interactive, _ := optBool(options, "interactive"); interactive {
		mode = "region"
	}
	path, err := screenshot.NewClient(s.conn).Capture(context.Background(), mode, "")
	switch {
	case err != nil:
		warnf("screenshot: capture failed: %v", err)
		return ResponseOther, Vardict{}, nil
	case path == "":
		return ResponseCancelled, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{"uri": dbus.MakeVariant(fileuri.FromPath(path))}, nil
}

// PickColor samples one screen pixel as an sRGB (ddd). The shell errors
// on a cancel as on a failure, and both reply cancelled.
func (s screenshotPortal) PickColor(_ dbus.ObjectPath, _, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	r, g, b, err := screenshot.NewClient(s.conn).PickColor(context.Background())
	if err != nil {
		warnf("screenshot: color pick failed/cancelled: %v", err)
		return ResponseCancelled, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{"color": dbus.MakeVariant(colorTuple{r, g, b})}, nil
}

// but the unreserved ones and '/'.
