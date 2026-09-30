package bar

import (
	"context"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/network"
)

// This file is the network dropdown's VPN section
// (dropdowns/network/vpn_connections): one row per NM VPN profile with
// its state glyph, its name, and its state or failure reason, a click
// toggling it.

// vpnStateIcon is a row's glyph (vpn_item.rs's state_icon): the
// dropdown's own, not the bar icon the user themes.
func vpnStateIcon(state network.VPNState) string {
	switch state {
	case network.VPNConnected:
		return "ld-lock-symbolic"
	case network.VPNConnecting:
		return "ld-refresh-cw-symbolic"
	}
	return "ld-unplug-symbolic"
}

// vpnStateLabel is the _network.ftl state wording.
func vpnStateLabel(state network.VPNState) string {
	switch state {
	case network.VPNConnected:
		return "Connected"
	case network.VPNConnecting:
		return "Connecting..."
	case network.VPNFailed:
		return "Connection failed"
	}
	return "Disconnected"
}

// vpnRowCaption is the row's second line: the failure reason as a
// sentence when there is one, the state otherwise.
func vpnRowCaption(row network.VPN) string {
	if row.Detail != "" {
		return sentence(row.Detail)
	}
	return vpnStateLabel(row.State)
}

// sentence capitalises a reason for its own line (vpn_item.rs): wayle's
// errors are written lowercase to read mid-sentence in a log; only the
// first letter is touched.
func sentence(reason string) string {
	reason = strings.TrimSpace(reason)
	first, size := utf8.DecodeRuneInString(reason)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(first)) + reason[size:]
}

// vpnSection builds the VPN rows, nil when NM holds no VPN.
func vpnSection(ctx ModuleContext, font render.Font, px float64) widget.Widget {
	if ctx.NetworkService == nil || ctx.NetworkService.VPN.IsEmpty() {
		return nil
	}
	vpn := ctx.NetworkService.VPN
	col := widget.NewBox(widget.Column, 4, 0)
	col.Append(widget.NewLabel(font, px, "VPN", mutedFg(ctx.Style.palette)), false)
	for _, row := range vpn.Entries() {
		uuid := row.UUID
		caption := widget.NewLabel(font, px*0.85, vpnRowCaption(row), mutedFg(ctx.Style.palette))
		info := widget.NewBox(widget.Column, 2, 0)
		info.Append(widget.NewLabel(font, px, row.Name, ctx.Style.fg), false)
		info.Append(caption, false)
		line := widget.NewBox(widget.Row, 10, 8)
		line.Append(widget.NewThemeIcon(vpnStateIcon(row.State), int(px)), false)
		line.Append(info, true)
		button := widget.NewButton(line, 4, 6)
		if ctx.Style != nil {
			button.BgHover = ctx.Style.buttonBgHover
			button.BgPressed = ctx.Style.buttonBgActive
		}
		button.OnClick = func() {
			go func() {
				if err := vpn.Toggle(context.Background(), uuid); err != nil {
					log.Printf("network: VPN toggle: %v", err)
				}
			}()
		}
		col.Append(button, false)
	}
	return col
}
