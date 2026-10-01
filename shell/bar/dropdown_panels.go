package bar

import (
	"context"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
)

// networkDropdown is the connectivity card: wifi, wired, and the
// radio state, from one NM snapshot.
func networkDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 6, 14)
	if ctx.Network == nil {
		col.Append(widget.NewLabel(font, px, "No NetworkManager", mutedFg(ctx.Style.palette)), false)
		return col
	}
	snap, err := ctx.Network.Read(context.Background())
	if err != nil {
		col.Append(widget.NewLabel(font, px, "NM unreachable", mutedFg(ctx.Style.palette)), false)
		return col
	}
	if snap.WifiEnabled {
		line := i18n.T("bar-network-disconnected")
		switch {
		case snap.WifiConnected:
			line = snap.WifiSSID + " (" + strconv.Itoa(int(snap.WifiStrength)) + "%)"
		case snap.WifiConnecting:
			line = i18n.T("bar-network-connecting")
		}
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-network-wifi")+": "+line, ctx.Style.fg), false)
	} else {
		col.Append(widget.NewLabel(font, px, "WiFi off", mutedFg(ctx.Style.palette)), false)
	}
	wired := i18n.T("bar-network-disconnected")
	if snap.WiredConnected {
		wired = i18n.T("bar-network-wired")
	} else if snap.WiredConnecting {
		wired = i18n.T("bar-network-connecting")
	}
	col.Append(widget.NewLabel(font, px, i18n.T("dropdown-network-ethernet")+": "+wired, ctx.Style.fg), false)
	if vpns := vpnSection(ctx, font, px); vpns != nil {
		col.Append(vpns, false)
	}
	return col
}
