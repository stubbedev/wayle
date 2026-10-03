package bar

import (
	"context"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/spawn"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/service/sysinfo"
	"github.com/stubbedev/wayle/service/upower"
	"github.com/stubbedev/wayle/shell/widgets"
	"github.com/stubbedev/wayle/styling"
)

// dashboardPoll paces what has no change signal while the dashboard is
// open: system stats, network rates, and the media position.
const dashboardPoll = time.Second

// dashboardView is the dashboard dropdown (dropdowns/dashboard): quick
// actions, the volume control, now playing, battery and network, the
// system rings, and the user session, following their services while
// open.
type dashboardView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	refreshers []func()
	pollers    []func()
	stops      []func()
	once       sync.Once
	stop       chan struct{}
}

func dashboardDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &dashboardView{ctx: ctx, font: font, px: px, stop: make(chan struct{})}
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "dashboard-dropdown")
	v.Append(v.header(), false)
	content := widget.NewBox(widget.Column, 10, 0)
	content.AddClass("dashboard-content-box")
	content.Append(v.quickActions(), false)
	if ctx.Pulse != nil {
		content.Append(v.controls(), false)
	}
	if ctx.Media != nil {
		content.Append(v.media(), false)
	}
	content.Append(v.infoRow(), false)
	content.Append(v.systemStats(), false)
	content.Append(userSessionSection(ctx), false)
	v.Append(dropdownScroll(content, ""), true)
	for _, refresh := range v.refreshers {
		refresh()
	}
	v.follow()
	return v
}

func (v *dashboardView) tint(token config.CssToken) render.Color {
	return tokenColor(v.ctx.Style.palette, token)
}

func (v *dashboardView) icon(name string, scale float64, color render.Color) *widget.Icon {
	icon := widget.NewThemeIcon(name, int(math.Round(v.px*scale)))
	icon.SetTint(color)
	return icon
}

func (v *dashboardView) label(text string, scale float64, color render.Color) *widget.Label {
	return widget.NewLabel(v.font, v.px*scale, text, color)
}

func (v *dashboardView) button(child widget.Widget, class string, onClick func()) *widget.Button {
	return dropdownButton(child, class, onClick)
}

// card is the "card dashboard-card" shell with its titled header.
func (v *dashboardView) card(iconName, title string, extra ...widget.Widget) *widget.Box {
	card := widget.NewBox(widget.Column, 8, 10)
	card.AddClass("card", "dashboard-card")
	header := widget.NewBox(widget.Row, 6, 0)
	header.AddClass("card-header")
	header.Append(v.icon(iconName, 1, v.tint(config.TokenFgMuted)), false)
	header.Append(v.label(title, 0.95, v.tint(config.TokenFgMuted)), true)
	for _, w := range extra {
		header.Append(w, false)
	}
	card.Append(header, false)
	return card
}

// header is the DropdownHeader with the open-settings action.
func (v *dashboardView) header() widget.Widget {
	settings := v.button(v.icon("ld-settings-symbolic", 1, v.tint(config.TokenFgMuted)), "dashboard-settings-btn", func() {
		// spawn_settings_app; the popover closes on the click.
		_ = spawn.Quiet("wayle-settings")
	})
	settings.SetTooltip(i18n.T("dropdown-dashboard-open-settings"))
	return dropdownHeader(v.font, v.px, "ld-layout-dashboard-symbolic", i18n.T("dropdown-dashboard-title"), settings)
}

// quickActionsState is what the quick actions cannot read back from a
// service: airplane mode and the radios it switched off.
var quickActionsState struct {
	sync.Mutex
	airplane, preWifi, preBT bool
}

// quickActions is quick_actions: wifi, bluetooth, airplane, do not
// disturb, idle inhibit, and power saver toggles.
func (v *dashboardView) quickActions() widget.Widget {
	card := widget.NewBox(widget.Column, 0, 10)
	card.AddClass("card", "dashboard-card")
	grid := widget.NewGrid(8, 4).SetColumnHomogeneous(true).SetRowHomogeneous(true)
	grid.AddClass("quick-actions")
	card.Append(grid, false)

	type toggle struct {
		btn  *widget.Button
		icon *widget.Icon
	}
	make1 := func(col, row int, iconName, labelID string, onClick func()) toggle {
		icon := v.icon(iconName, 1.3, v.ctx.Style.fg)
		col2 := widget.NewBox(widget.Column, 4, 0)
		col2.Append(icon, false)
		col2.Append(v.label(i18n.T(labelID), 0.8, v.ctx.Style.fg), false)
		b := v.button(col2, "quick-action", onClick)
		grid.Attach(b, col, row, 1, 1)
		return toggle{b, icon}
	}
	bctx := context.Background()
	var wifi, bt, airplane, dnd, idle, saver toggle
	var state struct {
		hasWifi, wifiOn, hasBT, btOn, dndOn, idleOn, hasPP, saverOn bool
	}
	wifiDev := func() bool { return v.ctx.Networking != nil && v.ctx.Networking.Wifi != nil }
	setWifi := func(on bool) {
		if !wifiDev() {
			return
		}
		go func() {
			if err := v.ctx.Networking.Wifi.SetEnabled(bctx, on); err != nil {
				log.Printf("dashboard: wifi toggle failed: %v", err)
			}
		}()
	}
	setBT := func(on bool) {
		if v.ctx.Bluetooth == nil {
			return
		}
		go func() {
			var err error
			if on {
				err = v.ctx.Bluetooth.Enable(bctx)
			} else {
				err = v.ctx.Bluetooth.Disable(bctx)
			}
			if err != nil {
				log.Printf("dashboard: bluetooth toggle failed: %v", err)
			}
		}()
	}
	wifi = make1(0, 0, "ld-wifi-symbolic", "dropdown-dashboard-wifi", func() { setWifi(!state.wifiOn) })
	bt = make1(1, 0, "ld-bluetooth-symbolic", "dropdown-dashboard-bluetooth", func() { setBT(!state.btOn) })
	airplane = make1(2, 0, "ld-plane-symbolic", "dropdown-dashboard-airplane", func() {
		// toggle_airplane: going on remembers and switches the radios
		// off; going off restores the ones that were on.
		s := &quickActionsState
		s.Lock()
		s.airplane = !s.airplane
		if s.airplane {
			s.preWifi, s.preBT = state.wifiOn, state.btOn
			if state.wifiOn {
				setWifi(false)
			}
			if state.btOn {
				setBT(false)
			}
		} else {
			if s.preWifi {
				setWifi(true)
			}
			if s.preBT {
				setBT(true)
			}
		}
		s.Unlock()
		v.refreshAll()
	})
	dnd = make1(0, 1, "ld-bell-symbolic", "dropdown-dashboard-dnd", func() {
		if v.ctx.Notifications != nil {
			v.ctx.Notifications.SetDND(!state.dndOn)
		}
	})
	idle = make1(1, 1, "ld-eye-symbolic", "dropdown-dashboard-idle-inhibit", func() {
		if s := v.ctx.IdleInhibit; s != nil {
			if s.Active() {
				s.Disable()
			} else {
				s.Enable(false)
			}
		}
	})
	saver = make1(2, 1, "ld-leaf-symbolic", "dropdown-dashboard-power-saver", func() {
		if v.ctx.PowerProfiles == nil {
			return
		}
		target := powerprofiles.ProfilePowerSaver
		if state.saverOn {
			target = powerprofiles.ProfileBalanced
		}
		go func() {
			if err := v.ctx.PowerProfiles.SetActive(bctx, target); err != nil {
				log.Printf("dashboard: power profile toggle failed: %v", err)
			}
		}()
	})

	v.refreshers = append(v.refreshers, func() {
		state.hasWifi = wifiDev()
		if v.ctx.Network != nil {
			if snap, err := v.ctx.Network.Read(bctx); err == nil {
				state.wifiOn = snap.WifiEnabled
			}
		}
		if v.ctx.Bluetooth != nil {
			bs := v.ctx.Bluetooth.State()
			state.hasBT, state.btOn = bs.Available, bs.Enabled
		}
		if v.ctx.Notifications != nil {
			state.dndOn = v.ctx.Notifications.DND()
		}
		if v.ctx.IdleInhibit != nil {
			state.idleOn = v.ctx.IdleInhibit.Active()
		}
		if v.ctx.PowerProfiles != nil {
			if snap, err := v.ctx.PowerProfiles.Read(bctx); err == nil && snap.Available {
				state.hasPP, state.saverOn = true, snap.Active == powerprofiles.ProfilePowerSaver
			}
		}
		quickActionsState.Lock()
		air := quickActionsState.airplane
		quickActionsState.Unlock()
		apply := func(t toggle, active, sensitive bool) {
			setClass(t.btn, "active", active)
			t.btn.SetEnabled(sensitive)
		}
		apply(wifi, state.wifiOn, state.hasWifi && !air)
		wifi.icon.SetThemeName(map[bool]string{true: "ld-wifi-symbolic", false: "ld-wifi-off-symbolic"}[state.wifiOn])
		apply(bt, state.btOn, state.hasBT && !air)
		bt.icon.SetThemeName(map[bool]string{true: "ld-bluetooth-symbolic", false: "ld-bluetooth-off-symbolic"}[state.btOn])
		apply(airplane, air, state.hasWifi || state.hasBT)
		apply(dnd, state.dndOn, v.ctx.Notifications != nil)
		dnd.icon.SetThemeName(map[bool]string{true: "ld-bell-off-symbolic", false: "ld-bell-symbolic"}[state.dndOn])
		apply(idle, state.idleOn, true)
		apply(saver, state.saverOn, state.hasPP)
	})
	return card
}

// controls is the volume card: mute, the debounced slider, and the
// output device's name.
func (v *dashboardView) controls() widget.Widget {
	card := v.card("ld-audio-lines-symbolic", i18n.T("dropdown-dashboard-volume"))
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("dashboard-slider-row")
	muteIcon := v.icon("ld-volume-2-symbolic", 1.1, v.ctx.Style.fg)
	var muted bool
	bctx := context.Background()
	mute := v.button(muteIcon, "dashboard-volume-icon", func() {
		target := !muted
		go func() {
			if err := v.ctx.Pulse.SetMuted(bctx, target); err != nil {
				log.Printf("dashboard: mute toggle failed: %v", err)
			}
		}()
	})
	row.Append(mute, false)
	slider := widgets.NewDebouncedSlider(0, v.font, v.px*0.9, v.tint(config.TokenFgMuted), v.ctx.Invoke)
	slider.AddClass("dashboard-volume-slider")
	slider.OnCommit = func(pct float64) {
		go func() {
			if err := v.ctx.Pulse.SetVolume(bctx, pct); err != nil {
				log.Printf("dashboard: volume set failed: %v", err)
			}
		}()
	}
	row.Append(slider, true)
	card.Append(row, false)
	device := v.label(i18n.T("dropdown-dashboard-no-device"), 0.85, v.tint(config.TokenFgSubtle))
	device.AddClass("controls-device")
	device.SetEllipsize(widget.EllipsizeEnd)
	device.SetAlignment(render.AlignEnd)
	card.Append(device, false)
	v.refreshers = append(v.refreshers, func() {
		sink, err := v.ctx.Pulse.DefaultSink(bctx)
		has := err == nil
		row.SetEnabled(has)
		if !has {
			device.SetText(i18n.T("dropdown-dashboard-no-device"))
			muteIcon.SetThemeName("ld-volume-x-symbolic")
			return
		}
		muted = sink.Muted
		device.SetText(sink.Description)
		slider.Set(sink.Volume.AveragePercentage())
		muteIcon.SetThemeName(map[bool]string{true: "ld-volume-x-symbolic", false: "ld-volume-2-symbolic"}[muted])
	})
	return card
}

// media is media_section: the active player's art, track, transport,
// and progress, or the empty state.
func (v *dashboardView) media() widget.Widget {
	src := v.ctx.Media
	var current mpris.Player
	var has bool
	bctx := context.Background()
	switchBtn := v.button(v.icon("ld-arrow-left-right-symbolic", 1, v.tint(config.TokenFgMuted)), "media-switch-btn", func() {
		// cycle_player: the next player after the active one.
		players := src.Players()
		if len(players) < 2 {
			return
		}
		i := 0
		for j, p := range players {
			if p.BusName == current.BusName {
				i = j
			}
		}
		if err := src.SetActive(players[(i+1)%len(players)].BusName); err != nil {
			log.Printf("dashboard: switch player failed: %v", err)
		}
	})
	card := v.card("ld-disc-3-symbolic", i18n.T("dropdown-dashboard-now-playing"), switchBtn)
	artPx := int(math.Round(v.px * 3.5))
	art := newFixedBox(artPx, artPx, nil)
	art.AddClass("dashboard-media-art")
	artURL := "\x00"
	title := v.label("", 1, v.ctx.Style.fg)
	artist := v.label("", 0.85, v.tint(config.TokenFgMuted))
	for _, l := range []*widget.Label{title, artist} {
		l.SetEllipsize(widget.EllipsizeEnd)
	}
	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("media-info")
	info.Append(title, false)
	info.Append(artist, false)
	fire := func(what string, cmd func(context.Context, string) error) {
		bus := current.BusName
		go func() {
			if err := cmd(bctx, bus); err != nil {
				log.Printf("dashboard: %s failed: %v", what, err)
			}
		}()
	}
	prev := v.button(v.icon("ld-skip-back-symbolic", 1, v.ctx.Style.fg), "media-btn", func() { fire("previous", src.Previous) })
	playIcon := v.icon("ld-play-symbolic", 1, v.ctx.Style.fg)
	play := v.button(playIcon, "media-btn", func() { fire("play-pause", src.PlayPause) })
	play.AddClass("play")
	next := v.button(v.icon("ld-skip-forward-symbolic", 1, v.ctx.Style.fg), "media-btn", func() { fire("next", src.Next) })
	controls := widget.NewBox(widget.Row, 2, 0)
	controls.AddClass("media-controls")
	controls.Append(prev, false)
	controls.Append(play, false)
	controls.Append(next, false)
	compact := widget.NewBox(widget.Row, 10, 0)
	compact.AddClass("media-compact")
	compact.Append(art, false)
	compact.Append(info, true)
	compact.Append(controls, false)

	seek := widgets.NewDebouncedSlider(0, nil, 0, 0, v.ctx.Invoke)
	elapsed := v.label("0:00", 0.8, v.tint(config.TokenFgSubtle))
	length := v.label("0:00", 0.8, v.tint(config.TokenFgSubtle))
	seek.OnCommit = func(pct float64) {
		if current.Length <= 0 {
			return
		}
		target := time.Duration(pct / 100 * float64(current.Length))
		bus := current.BusName
		go func() {
			if err := src.SetPosition(bctx, bus, target); err != nil {
				log.Printf("dashboard: seek failed: %v", err)
			}
		}()
	}
	times := widget.NewBox(widget.Row, 0, 0)
	times.AddClass("media-progress-times")
	times.Append(elapsed, true)
	times.Append(length, false)
	progress := widget.NewBox(widget.Column, 2, 0)
	progress.AddClass("media-progress")
	progress.Append(seek, false)
	progress.Append(times, false)
	player := widget.NewBox(widget.Column, 8, 0)
	player.Append(compact, false)
	player.Append(progress, false)

	empty := emptyState(v.font, v.px, "ld-music-symbolic", i18n.T("dropdown-dashboard-no-media-title"), i18n.T("dropdown-dashboard-no-media-description"))
	body := widget.NewStack()
	body.Add("player", player)
	body.Add("empty", empty)
	card.Append(body, false)

	setPosition := func(pos time.Duration) {
		elapsed.SetText(formatMediaDuration(pos))
		seek.Set(mediaProgress(pos, current.Length))
	}
	v.refreshers = append(v.refreshers, func() {
		current, has = src.Active()
		switchBtn.SetVisible(len(src.Players()) > 1)
		if !has {
			body.Show("empty")
			seek.Set(0)
			return
		}
		body.Show("player")
		title.SetText(current.Title)
		artist.SetText(current.Artist)
		if current.ArtURL != artURL {
			artURL = current.ArtURL
			art.SetChild(mediaArt(v.ctx, artURL, "ld-music-symbolic", int(v.px*1.4)))
		}
		playIcon.SetThemeName(map[bool]string{true: "ld-pause-symbolic", false: "ld-play-symbolic"}[current.State == mpris.StatePlaying])
		prev.SetEnabled(current.CanGoPrevious)
		next.SetEnabled(current.CanGoNext)
		seek.SetEnabled(current.CanSeek)
		length.SetText(lengthText(current.Length))
	})
	v.pollers = append(v.pollers, func() {
		if !has {
			return
		}
		bus := current.BusName
		go func() {
			ctx, cancel := context.WithTimeout(bctx, time.Second)
			defer cancel()
			if pos, err := src.Position(ctx, bus); err == nil {
				v.ctx.Invoke(func() { setPosition(pos) })
			}
		}()
	})
	return card
}

// infoRow is the battery and network cards side by side; without a
// battery the network card has the row.
func (v *dashboardView) infoRow() widget.Widget {
	row := widget.NewBox(widget.Row, 10, 0)
	row.AddClass("dashboard-info-row")
	if v.ctx.Battery != nil {
		row.Append(v.battery(), true)
	}
	row.Append(v.network(), true)
	return row
}

// dashboardBatteryIcon is battery_icon.
func dashboardBatteryIcon(percent float64, charging bool) string {
	switch {
	case charging:
		return "ld-battery-charging-symbolic"
	case percent > 75:
		return "ld-battery-full-symbolic"
	case percent > 50:
		return "ld-battery-medium-symbolic"
	case percent > 25:
		return "ld-battery-low-symbolic"
	}
	return "ld-battery-warning-symbolic"
}

// dashboardBatteryTime is time_remaining_label: "~1h 5m" or "~5m",
// empty without an estimate.
func dashboardBatteryTime(secs int64) string {
	if secs <= 0 {
		return ""
	}
	h, m := secs/3600, (secs%3600)/60
	if h > 0 {
		return i18n.T("dropdown-dashboard-battery-time-hm", i18n.Str("hours", strconv.FormatInt(h, 10)), i18n.Str("minutes", strconv.FormatInt(m, 10)))
	}
	return i18n.T("dropdown-dashboard-battery-time-m", i18n.Str("minutes", strconv.FormatInt(m, 10)))
}

// dashboardProfile is power_profile_label and power_profile_icon.
func dashboardProfile(active string) (label, icon string) {
	switch active {
	case powerprofiles.ProfilePowerSaver:
		return i18n.T("dropdown-dashboard-battery-profile-saver"), "ld-leaf-symbolic"
	case powerprofiles.ProfilePerformance:
		return i18n.T("dropdown-dashboard-battery-profile-performance"), "ld-zap-symbolic"
	}
	return i18n.T("dropdown-dashboard-battery-profile-balanced"), "ld-scale-symbolic"
}

// battery is battery_section: icon, percent, gauge, time left, and the
// power profile, tinted by the dashboard's battery thresholds.
func (v *dashboardView) battery() widget.Widget {
	card := v.card("ld-battery-full-symbolic", i18n.T("dropdown-dashboard-battery"))
	cfg := v.ctx.Config.Dashboard
	status := widget.NewBox(widget.Row, 8, 0)
	status.AddClass("battery-status")
	icon := v.icon("ld-battery-full-symbolic", 1.4, v.ctx.Style.fg)
	percent := v.label("", 1.3, v.ctx.Style.fg)
	status.Append(icon, false)
	status.Append(percent, false)
	card.Append(status, false)
	gauge := widget.NewProgressBar(0)
	card.Append(gauge, false)
	remaining := v.label("", 0.8, v.tint(config.TokenFgMuted))
	card.Append(remaining, false)
	profileRow := widget.NewBox(widget.Row, 6, 0)
	profileRow.AddClass("battery-profile")
	profileIcon := v.icon("ld-scale-symbolic", 0.9, v.tint(config.TokenFgMuted))
	profileLabel := v.label("", 0.8, v.tint(config.TokenFgMuted))
	profileRow.Append(profileIcon, false)
	profileRow.Append(profileLabel, false)
	card.Append(profileRow, false)
	bctx := context.Background()
	v.refreshers = append(v.refreshers, func() {
		dev, err := v.ctx.Battery.Read(bctx)
		if err != nil {
			return
		}
		pct := dev.Percentage
		warning := pct <= float64(cfg.BatteryWarning) && pct > float64(cfg.BatteryCritical)
		critical := pct <= float64(cfg.BatteryCritical)
		color := v.tint(config.TokenStatusSuccess)
		switch {
		case critical:
			color = v.tint(config.TokenStatusError)
		case warning:
			color = v.tint(config.TokenStatusWarning)
		}
		ink := v.ctx.Style.fg
		if warning || critical {
			ink = color
		}
		icon.SetThemeName(dashboardBatteryIcon(pct, dev.State == upower.StateCharging))
		icon.SetTint(ink)
		percent.SetText(strconv.FormatFloat(pct, 'f', 0, 64) + "%")
		percent.SetColor(ink)
		gauge.SetValue(pct / 100)
		gauge.Fill = color
		var secs int64
		switch dev.State {
		case upower.StateDischarging:
			secs = int64(dev.TimeToEmpty / time.Second)
		case upower.StateCharging:
			secs = int64(dev.TimeToFull / time.Second)
		}
		remaining.SetText(dashboardBatteryTime(secs))
		remaining.SetVisible(secs > 0)
		if v.ctx.PowerProfiles != nil {
			if snap, err := v.ctx.PowerProfiles.Read(bctx); err == nil && snap.Available {
				label, glyph := dashboardProfile(snap.Active)
				profileLabel.SetText(label)
				profileIcon.SetThemeName(glyph)
				profileRow.SetVisible(true)
				return
			}
		}
		profileRow.SetVisible(false)
	})
	return card
}

// dashboardSpeed is format_speed: KB/s with one decimal under 1024,
// else MB/s.
func dashboardSpeed(bytesPerSec uint64) (value string, mega bool) {
	kbps := float64(bytesPerSec) / 1024
	if kbps < 1024 {
		return strconv.FormatFloat(kbps, 'f', 1, 64), false
	}
	return strconv.FormatFloat(kbps/1024, 'f', 1, 64), true
}

// network is network_section: upload and download across every
// interface, "--" while disconnected.
func (v *dashboardView) network() widget.Widget {
	card := v.card("ld-wifi-symbolic", i18n.T("dropdown-dashboard-network"))
	speeds := widget.NewBox(widget.Row, 8, 0)
	speeds.AddClass("network-speeds")
	type stat struct {
		box          *widget.Box
		value, units *widget.Label
	}
	mk := func(arrow string) stat {
		s := stat{box: widget.NewBox(widget.Column, 2, 0)}
		s.box.AddClass("speed-stat")
		s.box.Append(v.icon(arrow, 0.9, v.tint(config.TokenFgMuted)), false)
		s.value = v.label("--", 1.1, v.ctx.Style.fg)
		s.units = v.label(i18n.T("dropdown-dashboard-network-speed-kbs"), 0.75, v.tint(config.TokenFgSubtle))
		s.box.Append(s.value, false)
		s.box.Append(s.units, false)
		speeds.Append(s.box, true)
		return s
	}
	up, down := mk("ld-arrow-up-symbolic"), mk("ld-arrow-down-symbolic")
	card.Append(speeds, false)
	var prev map[string]sysinfo.NetTotals
	var prevAt time.Time
	var connected bool
	unit := func(mega bool) string {
		if mega {
			return i18n.T("dropdown-dashboard-network-speed-mbs")
		}
		return i18n.T("dropdown-dashboard-network-speed-kbs")
	}
	show := func(rx, tx uint64) {
		for _, s := range []struct {
			stat
			rate uint64
		}{{up, tx}, {down, rx}} {
			value, mega := dashboardSpeed(s.rate)
			if !connected {
				value = "--"
			}
			s.value.SetText(value)
			s.units.SetText(unit(mega))
			muted := v.ctx.Style.fg
			if !connected {
				muted = v.tint(config.TokenFgSubtle)
			}
			s.value.SetColor(muted)
			setClass(s.box, "muted", !connected)
		}
	}
	v.refreshers = append(v.refreshers, func() {
		connected = false
		if v.ctx.Network != nil {
			if snap, err := v.ctx.Network.Read(context.Background()); err == nil {
				connected = snap.WiredConnected || snap.WifiEnabled && snap.WifiSSID != ""
			}
		}
		show(0, 0)
	})
	v.pollers = append(v.pollers, func() {
		totals, err := sysinfo.ReadNetTotals()
		if err != nil {
			return
		}
		now := time.Now()
		var rx, tx uint64
		if prev != nil {
			for name, cur := range totals {
				if old, ok := prev[name]; ok {
					if r, err := sysinfo.NetRate(name, old, cur, now.Sub(prevAt).Seconds()); err == nil {
						rx += r.RxPerSec
						tx += r.TxPerSec
					}
				}
			}
		}
		prev, prevAt = totals, now
		show(rx, tx)
	})
	return card
}

// dashboardThresholdColor is threshold_color: error at or past the
// error mark, warning at or past the warning mark, else success.
func dashboardThresholdColor(value, warning, errorAt float32) config.CssToken {
	switch {
	case value >= errorAt:
		return config.TokenStatusError
	case value >= warning:
		return config.TokenStatusWarning
	}
	return config.TokenStatusSuccess
}

// systemStats is system_stats: CPU, RAM, root disk, and CPU temperature
// rings.
func (v *dashboardView) systemStats() widget.Widget {
	card := v.card("ld-activity-symbolic", i18n.T("dropdown-dashboard-system"))
	cfg := v.ctx.Config.Dashboard
	scale := float64(v.ctx.Config.Styling.Scale)
	if scale <= 0 {
		scale = 1
	}
	size := int(math.Round(4 * styling.RemBase * scale))
	stroke := int(math.Round(4 * scale))
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("system-stats-inline")
	ring := func(labelID string) *progressRing {
		r := newProgressRing(size, stroke, v.font, v.px*0.85, v.ctx.Style.fg)
		col := widget.NewBox(widget.Column, 4, 0)
		col.AddClass("stat-inline")
		col.Append(r, false)
		col.Append(v.label(i18n.T(labelID), 0.75, v.tint(config.TokenFgMuted)), false)
		row.Append(col, true)
		return r
	}
	cpuRing, memRing, diskRing, tempRing := ring("dropdown-dashboard-cpu"), ring("dropdown-dashboard-ram"), ring("dropdown-dashboard-disk"), ring("dropdown-dashboard-temp")
	card.Append(row, false)
	usage := func(r *progressRing, pct float32) {
		r.set(float64(pct/100), strconv.FormatFloat(float64(pct), 'f', 0, 32)+"%", v.tint(dashboardThresholdColor(pct, cfg.UsageWarning, cfg.UsageError)))
	}
	reader := &sysinfo.CPUReader{Sensor: v.ctx.Config.CPU.TempSensor}
	poll := func() {
		if cpu, err := reader.Read(); err == nil {
			usage(cpuRing, cpu.UsagePercent)
			if cpu.HasTemperature {
				c := cpu.TemperatureC
				tempRing.set(float64(min(max(c/100, 0), 1)), strconv.FormatFloat(float64(c), 'f', 0, 32)+"°", v.tint(dashboardThresholdColor(c, cfg.TempWarning, cfg.TempError)))
			}
		}
		if mem, err := sysinfo.ReadMemory(); err == nil {
			pct, _ := memoryPercents(mem)
			usage(memRing, pct)
		}
		root := float32(0)
		for _, d := range sysinfo.Disks() {
			if d.MountPoint == "/" {
				root = float32(d.UsedBytes) / float32(d.TotalBytes) * 100
			}
		}
		usage(diskRing, root)
	}
	v.refreshers = append(v.refreshers, poll)
	v.pollers = append(v.pollers, poll)
	return card
}

// refreshAll re-reads every section.
func (v *dashboardView) refreshAll() {
	for _, refresh := range v.refreshers {
		refresh()
	}
}

// follow keeps the dashboard current while open: every service change
// re-reads the sections, and the poll paces stats, rates, and the
// media position.
func (v *dashboardView) follow() {
	if v.ctx.App == nil {
		return
	}
	var feeds []<-chan struct{}
	add := func(ch <-chan struct{}, stop func()) {
		feeds = append(feeds, ch)
		v.stops = append(v.stops, stop)
	}
	life, cancel := context.WithCancel(context.Background())
	v.stops = append(v.stops, cancel)
	if v.ctx.Media != nil {
		add(v.ctx.Media.Subscribe())
	}
	if v.ctx.Bluetooth != nil {
		add(v.ctx.Bluetooth.Subscribe())
	}
	if v.ctx.IdleInhibit != nil {
		add(v.ctx.IdleInhibit.Changes())
	}
	if v.ctx.Notifications != nil {
		events, stop := v.ctx.Notifications.Subscribe()
		ticks := make(chan struct{}, 1)
		go func() {
			for range events {
				select {
				case ticks <- struct{}{}:
				default:
				}
			}
		}()
		add(ticks, stop)
	}
	for _, sub := range []func(context.Context) (<-chan struct{}, func(), error){
		subscribeOf(v.ctx.Pulse), subscribeOf(v.ctx.Network), subscribeOf(v.ctx.PowerProfiles), subscribeOf(v.ctx.Battery),
	} {
		if sub == nil {
			continue
		}
		if ch, stop, err := sub(life); err == nil {
			add(ch, stop)
		}
	}
	merged := make(chan struct{}, 1)
	for _, ch := range feeds {
		go func() {
			for range ch {
				select {
				case merged <- struct{}{}:
				default:
				}
			}
		}()
	}
	go func() {
		ticker := time.NewTicker(dashboardPoll)
		defer ticker.Stop()
		for {
			select {
			case <-v.stop:
				return
			case <-merged:
				v.ctx.Invoke(v.refreshAll)
			case <-ticker.C:
				v.ctx.Invoke(func() {
					for _, poll := range v.pollers {
						poll()
					}
				})
			}
		}
	}()
}

// subscriber is the Subscribe seam the pulse, network, power-profiles,
// and battery sources share.
type subscriber interface {
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}

func subscribeOf[T subscriber](s T) func(context.Context) (<-chan struct{}, func(), error) {
	var zero T
	if any(s) == any(zero) {
		return nil
	}
	return s.Subscribe
}

// dropdownClosed implements dropdownCloser: stop following.
func (v *dashboardView) dropdownClosed() {
	v.once.Do(func() {
		close(v.stop)
		for _, stop := range v.stops {
			stop()
		}
	})
}
