package bar

import (
	"context"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/mail"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/service/weather"
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
	cfg := ctx.Config.Network
	if snap.WifiEnabled {
		line := cfg.Disconnected
		switch {
		case snap.WifiConnected:
			line = snap.WifiSSID + " (" + strconv.Itoa(int(snap.WifiStrength)) + "%)"
		case snap.WifiConnecting:
			line = cfg.Connecting
		}
		col.Append(widget.NewLabel(font, px, "WiFi: "+line, ctx.Style.fg), false)
	} else {
		col.Append(widget.NewLabel(font, px, "WiFi off", mutedFg(ctx.Style.palette)), false)
	}
	wired := cfg.Disconnected
	if snap.WiredConnected {
		wired = cfg.Wired
	} else if snap.WiredConnecting {
		wired = cfg.Connecting
	}
	col.Append(widget.NewLabel(font, px, "Wired: "+wired, ctx.Style.fg), false)
	return col
}

// mediaDropdown is the now-playing card: the active player's track.
// Transport controls wait for the MPRIS control surface.
func mediaDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 4, 14)
	if ctx.Media == nil {
		col.Append(widget.NewLabel(font, px, "No MPRIS", mutedFg(ctx.Style.palette)), false)
		return col
	}
	track, err := ctx.Media.Active(context.Background())
	if err != nil {
		col.Append(widget.NewLabel(font, px, "No players", mutedFg(ctx.Style.palette)), false)
		return col
	}
	if track.Title != "" {
		col.Append(widget.NewLabel(font, px*1.3, track.Title, ctx.Style.fg), false)
	} else {
		col.Append(widget.NewLabel(font, px*1.3, "Nothing playing", ctx.Style.fg), false)
	}
	if track.Artist != "" {
		col.Append(widget.NewLabel(font, px, track.Artist, mutedFg(ctx.Style.palette)), false)
	}
	if track.Album != "" {
		col.Append(widget.NewLabel(font, px, track.Album, mutedFg(ctx.Style.palette)), false)
	}
	if track.Player != "" {
		col.Append(widget.NewLabel(font, px, "via "+track.Player, mutedFg(ctx.Style.palette)), false)
	}
	return col
}

// notificationDropdown is the notification card: the DND toggle row
// above the stored history, each row dismissable, with a clear-all.
func notificationDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 6, 12)
	svc := ctx.Notifications
	if svc == nil {
		col.Append(widget.NewLabel(font, px, "No notification service", mutedFg(ctx.Style.palette)), false)
		return col
	}
	dndLabel := "Do Not Disturb"
	if svc.DND() {
		dndLabel = "Do Not Disturb (on)"
	}
	col.Append(dropdownRow(ctx, font, px, dndLabel, "ld-bell-off-symbolic", func() { svc.ToggleDND() }), false)
	notifs := svc.Notifications()
	if len(notifs) == 0 {
		col.Append(widget.NewLabel(font, px, "No notifications", mutedFg(ctx.Style.palette)), false)
		return col
	}
	col.Append(dropdownRow(ctx, font, px, "Clear all", "ld-trash-2-symbolic", func() { svc.DismissAll() }), false)
	for _, n := range notifs {
		summary := n.Summary
		if n.AppName != "" {
			summary = n.AppName + ": " + n.Summary
		}
		col.Append(dropdownRow(ctx, font, px, summary, "ld-bell-symbolic", func() {
			svc.Close(n.ID, notifications.Dismissed)
		}), false)
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
	toggle := "Start recording"
	if snap.Status != recorder.StatusIdle {
		toggle = "Stop recording"
	}
	col.Append(dropdownRow(ctx, font, px, toggle, "ld-circle-dot-symbolic", func() { ctx.Recorder.Toggle() }), false)
	if snap.Status != recorder.StatusIdle {
		col.Append(widget.NewLabel(font, px, "Recording: "+recorder.FormatElapsed(snap.ElapsedSecs), ctx.Style.fg), false)
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
		col.Append(widget.NewLabel(font, px, "Treeman unavailable", mutedFg(ctx.Style.palette)), false)
		return col
	}
	return dropdownStatus(ctx, "Worktrees", []string{
		"Total: " + strconv.FormatUint(uint64(status.Total), 10),
		"Stable: " + strconv.FormatUint(uint64(status.Stable), 10),
		"Up: " + strconv.FormatUint(uint64(status.Up), 10),
		"Down: " + strconv.FormatUint(uint64(status.Down), 10),
		"Failed: " + strconv.FormatUint(uint64(status.Failed), 10),
	})
}

// mailDropdown is the mail card: one count per configured account.
func mailDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 6, 14)
	var accounts []mail.AccountUnread
	if ctx.Mail != nil {
		accounts = ctx.Mail.State().Accounts
	}
	if len(accounts) == 0 {
		col.Append(widget.NewLabel(font, px, "No accounts configured", mutedFg(ctx.Style.palette)), false)
		return col
	}
	for _, account := range accounts {
		col.Append(widget.NewLabel(font, px, account.Name+": "+strconv.FormatUint(uint64(account.Count), 10), ctx.Style.fg), false)
	}
	return col
}

// weatherDropdownClient resolves the weather client for the panel;
// nil when no location is configured.
func weatherDropdownClient(ctx ModuleContext) *weather.Client {
	if ctx.Config.Weather.Location == "" {
		return nil
	}
	return weather.NewClient()
}

// weatherDropdown is the weather card: the current conditions for the
// configured location.
func weatherDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 6, 14)
	client := weatherDropdownClient(ctx)
	if client == nil {
		col.Append(widget.NewLabel(font, px, "No location configured", mutedFg(ctx.Style.palette)), false)
		return col
	}
	lat, lon, err := client.Geocode(context.Background(), ctx.Config.Weather.Location)
	if err != nil {
		col.Append(widget.NewLabel(font, px, "Geocode failed", mutedFg(ctx.Style.palette)), false)
		return col
	}
	current, err := client.FetchForecast(context.Background(), lat, lon)
	if err != nil {
		col.Append(widget.NewLabel(font, px, "Forecast failed", mutedFg(ctx.Style.palette)), false)
		return col
	}
	col.Append(widget.NewLabel(font, px*1.6, strconv.FormatFloat(current.TempC, 'f', 0, 64)+"°C "+current.Condition.Label(), ctx.Style.fg), false)
	lines := []string{
		"Feels like " + strconv.FormatFloat(current.FeelsLikeC, 'f', 0, 64) + "°C",
		"Humidity " + strconv.Itoa(current.Humidity) + "%",
	}
	if current.HasHigh {
		lines = append(lines, "High "+strconv.FormatFloat(current.HighC, 'f', 0, 64)+"°")
	}
	if current.HasLow {
		lines = append(lines, "Low "+strconv.FormatFloat(current.LowC, 'f', 0, 64)+"°")
	}
	for _, line := range lines {
		col.Append(widget.NewLabel(font, px, line, mutedFg(ctx.Style.palette)), false)
	}
	return col
}
