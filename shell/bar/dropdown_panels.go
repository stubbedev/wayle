package bar

import (
	"context"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/recorder"
)

// dropdownRows builds a titled stack of status lines.
func dropdownStatus(ctx ModuleContext, title string, lines []string) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 4, 14)
	col.Append(widget.NewLabel(font, px*1.2, title, ctx.Style.fg), false)
	if len(lines) == 0 {
		col.Append(widget.NewLabel(font, px, "Nothing to show", mutedFg(ctx.Style.palette)), false)
		return col
	}
	for _, line := range lines {
		col.Append(widget.NewLabel(font, px, line, mutedFg(ctx.Style.palette)), false)
	}
	return col
}

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

// recorderDropdown is the recorder card: a toggle row plus the state
// and output path while a recording runs.
func recorderDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 6, 12)
	if ctx.Recorder == nil {
		col.Append(widget.NewLabel(font, px, "No recorder", mutedFg(ctx.Style.palette)), false)
		return col
	}
	snap := ctx.Recorder.Snapshot()
	toggle := i18n.T("dropdown-recorder-record")
	if snap.Status != recorder.StatusIdle {
		toggle = i18n.T("dropdown-recorder-stop")
	}
	col.Append(dropdownRow(ctx, font, px, toggle, "ld-circle-dot-symbolic", func() { ctx.Recorder.Toggle() }), false)
	if snap.Status != recorder.StatusIdle {
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-recorder-recording")+": "+recorder.FormatElapsed(snap.ElapsedSecs), ctx.Style.fg), false)
	}
	if snap.OutputPath != "" {
		col.Append(widget.NewLabel(font, px, snap.OutputPath, mutedFg(ctx.Style.palette)), false)
	}
	return col
}

// treemanDropdown is the worktree health card: the bucket counts.
func treemanDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 6, 14)
	if ctx.Treeman == nil {
		col.Append(widget.NewLabel(font, px, "No treeman", mutedFg(ctx.Style.palette)), false)
		return col
	}
	status, err := ctx.Treeman.Read(context.Background())
	if err != nil || status == nil {
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-treeman-empty-title"), mutedFg(ctx.Style.palette)), false)
		return col
	}
	// views.rs renders each bucket chip as "{count} {bucket}".
	bucket := func(count uint32, id string) string {
		return strconv.FormatUint(uint64(count), 10) + " " + i18n.T(id)
	}
	return dropdownStatus(ctx, i18n.T("dropdown-treeman-title"), []string{
		"Total: " + strconv.FormatUint(uint64(status.Total), 10),
		bucket(status.Stable, "dropdown-treeman-bucket-stable"),
		bucket(status.Up, "dropdown-treeman-bucket-up"),
		bucket(status.Down, "dropdown-treeman-bucket-down"),
		bucket(status.Failed, "dropdown-treeman-bucket-failed"),
	})
}
