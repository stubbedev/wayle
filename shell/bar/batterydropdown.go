package bar

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"sync"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/service/upower"
)

// batteryCharging is battery_section/methods.rs's is_charging.
func batteryCharging(state upower.DeviceState) bool {
	return state == upower.StateCharging || state == upower.StatePendingCharge
}

// batteryStateLabel is methods.rs's state_label: critical first, then
// charging, plugged in, or on battery.
func batteryStateLabel(dev upower.Device) string {
	switch {
	case dev.WarningLevel.Low():
		return i18n.T("dropdown-battery-critical")
	case batteryCharging(dev.State):
		return i18n.T("dropdown-battery-charging")
	case dev.State == upower.StateFullyCharged:
		return i18n.T("dropdown-battery-plugged-in")
	default:
		return i18n.T("dropdown-battery-on-battery")
	}
}

// batteryTimeDisplay is methods.rs's time_display: the time until full
// while charging, else the time remaining; empty when unknown.
func batteryTimeDisplay(dev upower.Device) string {
	remaining, id := dev.TimeToEmpty, "dropdown-battery-time-remaining"
	if batteryCharging(dev.State) {
		remaining, id = dev.TimeToFull, "dropdown-battery-time-until-full"
	}
	seconds := int64(remaining.Seconds())
	if seconds <= 0 {
		return ""
	}
	hours, minutes := seconds/3600, (seconds%3600)/60
	duration := i18n.T("dropdown-battery-duration-m", i18n.Str("minutes", strconv.FormatInt(minutes, 10)))
	if hours > 0 {
		duration = i18n.T("dropdown-battery-duration-hm",
			i18n.Str("hours", strconv.FormatInt(hours, 10)),
			i18n.Str("minutes", fmt.Sprintf("%02d", minutes)))
	}
	return i18n.T(id, i18n.Str("duration", duration))
}

// formatWatts is helpers.rs's format_watts: one decimal under 10W.
func formatWatts(watts float64) string {
	if watts < 10 {
		return strconv.FormatFloat(watts, 'f', 1, 64) + "W"
	}
	return strconv.FormatFloat(watts, 'f', 0, 64) + "W"
}

// formatWattHours is format_watt_hours.
func formatWattHours(wh float64) string {
	if wh < 10 {
		return strconv.FormatFloat(wh, 'f', 1, 64) + " Wh"
	}
	return strconv.FormatFloat(wh, 'f', 0, 64) + " Wh"
}

// batteryLevelClass is gauge_class and hero_pct_class: "crit" at a low
// warning level, "warn" at or under 20%, else fallback ("good" for the
// gauge, "" for the hero percentage).
func batteryLevelClass(dev upower.Device, fallback string) string {
	switch {
	case dev.WarningLevel.Low():
		return "crit"
	case dev.Percentage <= 20:
		return "warn"
	}
	return fallback
}

// heroStateClass is hero_state_class.
func heroStateClass(dev upower.Device) string {
	switch {
	case dev.WarningLevel.Low():
		return "crit"
	case batteryCharging(dev.State):
		return "good"
	}
	return ""
}

// healthClass is health_class.
func healthClass(capacity float64) string {
	switch {
	case capacity <= 0:
		return "unknown"
	case capacity >= 80:
		return "good"
	case capacity >= 50:
		return "fair"
	}
	return "poor"
}

// healthValue is health_value.
func healthValue(capacity float64) string {
	if capacity <= 0 {
		return "--"
	}
	return strconv.FormatFloat(capacity, 'f', 0, 64) + "%"
}

// resumeThreshold is resume_threshold: five under the limit, floored
// at zero.
func resumeThreshold(end uint32) uint32 {
	if end < 5 {
		return 0
	}
	return end - 5
}

// batteryVariantToken colors the stylesheet's variant classes, which
// the Go-painted dropdown cannot read from CSS.
var batteryVariantToken = map[string]config.CssToken{
	"crit":    config.TokenStatusError,
	"warn":    config.TokenStatusWarning,
	"good":    config.TokenStatusSuccess,
	"fair":    config.TokenStatusWarning,
	"poor":    config.TokenStatusError,
	"unknown": config.TokenFgSubtle,
}

// batteryVariants is every variant class a battery label may carry.
var batteryVariants = []string{"crit", "warn", "good", "fair", "poor", "unknown"}

// batteryProfile is one segment of the power-profile control.
type batteryProfile struct {
	name, icon, label string
}

var batteryProfiles = []batteryProfile{
	{powerprofiles.ProfilePowerSaver, "ld-leaf-symbolic", "dropdown-battery-profile-saver"},
	{powerprofiles.ProfileBalanced, "ld-scale-symbolic", "dropdown-battery-profile-balanced"},
	{powerprofiles.ProfilePerformance, "ld-rocket-symbolic", "dropdown-battery-profile-performance"},
}

// batteryView is the battery dropdown: the battery section (hero,
// gauge, details, charge limit, or the empty state) over the power
// profile section, each following its service while open.
type batteryView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	body *widget.Stack

	heroPct, heroState, heroTime, heroInput *widget.Label
	gauge                                   *widget.ProgressBar
	drawValue, drawLabel                    *widget.Label
	capacityValue, capacityLabel            *widget.Label
	healthDot, healthText                   *widget.Label
	chargeCard, chargeUnsupported           *widget.Box
	chargeTitle, chargeSubtitle             *widget.Label
	chargeSwitch                            *widget.Switch
	// syncing marks programmatic switch moves (block_signal).
	syncing bool

	profileButtons      map[string]*widget.Button
	profilesUnavailable *widget.Box
	activeProfile       string

	once   sync.Once
	cancel context.CancelFunc
}

func batteryDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &batteryView{ctx: ctx, font: font, px: px, cancel: func() {}}
	v.Box = widget.NewBox(widget.Column, 12, 14)
	v.AddClass("dropdown", "battery-dropdown")
	v.Append(dropdownHeader(ctx, font, px, "ld-battery-full-symbolic", i18n.T("dropdown-battery-title")), false)
	v.body = widget.NewStack()
	v.body.Add("empty", emptyState(ctx, font, px, "ld-unplug-symbolic",
		i18n.T("dropdown-battery-no-battery-title"), i18n.T("dropdown-battery-no-battery-description")))
	v.body.Add("battery", v.batterySection())
	v.Append(v.body, false)
	v.Append(v.profileSection(), false)
	v.applyDevice(v.readDevice(context.Background()))
	v.applyProfiles(v.readProfiles(context.Background()))
	v.follow()
	return v
}

func (v *batteryView) label(text string, scale float64, token config.CssToken, class string) *widget.Label {
	l := widget.NewLabel(v.font, v.px*scale, text, tokenColor(v.ctx.Style.palette, token))
	l.AddClass(class)
	return l
}

// setVariant swaps l's variant class to class ("" for none) and its
// color to the variant's token, or fallback.
func (v *batteryView) setVariant(l *widget.Label, class string, fallback config.CssToken) {
	l.RemoveClass(batteryVariants...)
	token := fallback
	if class != "" {
		l.AddClass(class)
		token = batteryVariantToken[class]
	}
	l.SetColor(tokenColor(v.ctx.Style.palette, token))
}

// batterySection builds battery_content.
func (v *batteryView) batterySection() widget.Widget {
	col := widget.NewBox(widget.Column, 10, 0)

	hero := widget.NewBox(widget.Row, 12, 0)
	hero.AddClass("battery-hero")
	v.heroPct = v.label("", 2.4, config.TokenFgDefault, "battery-hero-pct")
	hero.Append(v.heroPct, false)
	meta := widget.NewBox(widget.Column, 2, 0)
	meta.AddClass("battery-hero-meta")
	v.heroState = v.label("", 1, config.TokenFgDefault, "battery-hero-state")
	v.heroTime = v.label("", 0.85, config.TokenFgMuted, "battery-hero-time")
	v.heroInput = v.label("", 0.85, config.TokenFgMuted, "battery-hero-time")
	meta.Append(v.heroState, false)
	meta.Append(v.heroTime, false)
	meta.Append(v.heroInput, false)
	hero.Append(meta, true)
	col.Append(hero, false)

	v.gauge = widget.NewProgressBar(0)
	v.gauge.AddClass("battery-gauge")
	v.gauge.Trough = tokenColor(v.ctx.Style.palette, config.TokenBgSurfaceElevated)
	col.Append(v.gauge, false)

	details := widget.NewBox(widget.Row, 8, 0)
	details.AddClass("battery-details")
	detail := func(value, label *widget.Label, align render.Alignment) *widget.Box {
		box := widget.NewBox(widget.Column, 2, 0)
		box.AddClass("battery-detail")
		value.SetAlignment(align)
		label.SetAlignment(align)
		box.Append(value, false)
		box.Append(label, false)
		return box
	}
	v.drawValue = v.label("", 1, config.TokenFgDefault, "battery-detail-value")
	v.drawLabel = v.label("", 0.8, config.TokenFgMuted, "battery-detail-label")
	details.Append(detail(v.drawValue, v.drawLabel, render.AlignStart), true)
	v.capacityValue = v.label("", 1, config.TokenFgDefault, "battery-detail-value")
	v.capacityLabel = v.label("", 0.8, config.TokenFgMuted, "battery-detail-label")
	details.Append(detail(v.capacityValue, v.capacityLabel, render.AlignCenter), true)
	health := widget.NewBox(widget.Row, 4, 0)
	v.healthDot = v.label("●", 0.7, config.TokenFgSubtle, "health-dot")
	v.healthText = v.label("", 1, config.TokenFgDefault, "battery-detail-value")
	health.Append(widget.NewSpacer(0, 0), true)
	health.Append(v.healthDot, false)
	health.Append(v.healthText, false)
	healthBox := widget.NewBox(widget.Column, 2, 0)
	healthBox.AddClass("battery-detail")
	healthBox.Append(health, false)
	healthLabel := v.label(i18n.T("dropdown-battery-health"), 0.8, config.TokenFgMuted, "battery-detail-label")
	healthLabel.SetAlignment(render.AlignEnd)
	healthBox.Append(healthLabel, false)
	details.Append(healthBox, true)
	col.Append(details, false)

	col.Append(v.label(i18n.T("dropdown-battery-charge-limit"), 0.85, config.TokenFgMuted, "section-label"), false)
	v.chargeCard = widget.NewBox(widget.Row, 8, 10)
	v.chargeCard.AddClass("charge-limit")
	info := widget.NewBox(widget.Column, 2, 0)
	v.chargeTitle = v.label("", 1, config.TokenFgDefault, "charge-limit-title")
	v.chargeSubtitle = v.label("", 0.85, config.TokenFgMuted, "charge-limit-subtitle")
	info.Append(v.chargeTitle, false)
	info.Append(v.chargeSubtitle, false)
	v.chargeCard.Append(info, true)
	v.chargeSwitch = widget.NewSwitch(false)
	v.chargeSwitch.OnChanged = v.chargeToggled
	v.chargeCard.Append(v.chargeSwitch, false)
	col.Append(v.chargeCard, false)
	v.chargeUnsupported = v.infoNote("charge-limit-not-supported", "charge-limit-info-icon",
		"charge-limit-info-text", i18n.T("dropdown-battery-charge-limit-not-supported"))
	col.Append(v.chargeUnsupported, false)
	return col
}

// infoNote is the info-icon row the Rust sections show when a feature
// is missing.
func (v *batteryView) infoNote(class, iconClass, textClass, text string) *widget.Box {
	row := widget.NewBox(widget.Row, 6, 0)
	row.AddClass(class)
	icon := widget.NewThemeIcon("ld-info-symbolic", int(v.px))
	icon.SetTint(tokenColor(v.ctx.Style.palette, config.TokenFgMuted))
	icon.AddClass(iconClass)
	row.Append(icon, false)
	text2 := v.label(text, 0.85, config.TokenFgMuted, textClass)
	text2.SetWrap(true)
	row.Append(text2, true)
	return row
}

// profileSection builds PowerProfileSection: the label, the three
// segments, and the daemon-missing note.
func (v *batteryView) profileSection() widget.Widget {
	col := widget.NewBox(widget.Column, 8, 0)
	col.Append(v.label(i18n.T("dropdown-battery-power-profile"), 0.85, config.TokenFgMuted, "section-label"), false)
	seg := widget.NewBox(widget.Row, 4, 0)
	seg.AddClass("profile-seg")
	v.profileButtons = make(map[string]*widget.Button, len(batteryProfiles))
	for _, p := range batteryProfiles {
		content := widget.NewBox(widget.Row, 6, 0)
		content.AddClass("profile-seg-btn-content")
		icon := widget.NewThemeIcon(p.icon, int(v.px))
		icon.SetTint(v.ctx.Style.fg)
		icon.AddClass("profile-seg-icon")
		content.Append(widget.NewSpacer(0, 0), true)
		content.Append(icon, false)
		content.Append(widget.NewLabel(v.font, v.px*0.9, i18n.T(p.label), v.ctx.Style.fg), false)
		content.Append(widget.NewSpacer(0, 0), true)
		name := p.name
		b := dropdownButton(v.ctx, content, "profile-seg-btn", func() { v.selectProfile(name) })
		v.profileButtons[name] = b
		seg.Append(b, true)
	}
	col.Append(seg, false)
	v.profilesUnavailable = v.infoNote("power-profile-not-available", "power-profile-info-icon",
		"power-profile-info-text", i18n.T("dropdown-battery-power-profile-not-available"))
	col.Append(v.profilesUnavailable, false)
	return col
}

func (v *batteryView) readDevice(ctx context.Context) (upower.Device, bool) {
	if v.ctx.Battery == nil {
		return upower.Device{}, false
	}
	dev, err := v.ctx.Battery.Read(ctx)
	if err != nil {
		log.Printf("battery: %v", err)
		return upower.Device{}, false
	}
	return dev, true
}

// batteryRead is one device read: ok is false when the read failed.
type batteryRead struct {
	dev upower.Device
	ok  bool
}

// applyDevice is refresh_battery_state plus the view's #[watch]es.
func (v *batteryView) applyDevice(dev upower.Device, ok bool) {
	if !ok || !dev.IsPresent {
		v.body.Show("empty")
		return
	}
	v.body.Show("battery")
	charging := batteryCharging(dev.State)

	v.heroPct.SetText(strconv.Itoa(int(dev.Percentage)) + "%")
	v.setVariant(v.heroPct, batteryLevelClass(dev, ""), config.TokenFgDefault)
	v.heroState.SetText(batteryStateLabel(dev))
	v.setVariant(v.heroState, heroStateClass(dev), config.TokenFgDefault)
	remaining := batteryTimeDisplay(dev)
	v.heroTime.SetText(remaining)
	v.heroTime.SetVisible(remaining != "")
	v.heroInput.SetText(i18n.T("dropdown-battery-input-watts", i18n.Str("watts", formatWatts(dev.EnergyRate))))
	v.heroInput.SetVisible(charging && dev.EnergyRate > 0)

	v.gauge.SetValue(min(max(dev.Percentage, 0), 100) / 100)
	v.gauge.RemoveClass(batteryVariants...)
	gaugeClass := batteryLevelClass(dev, "good")
	v.gauge.AddClass(gaugeClass)
	v.gauge.Fill = tokenColor(v.ctx.Style.palette, batteryVariantToken[gaugeClass])

	v.drawValue.SetText(formatWatts(dev.EnergyRate))
	v.capacityValue.SetText(formatWattHours(dev.EnergyFull))
	if charging {
		v.drawLabel.SetText(i18n.T("dropdown-battery-input"))
		v.capacityLabel.SetText(i18n.T("dropdown-battery-charged"))
	} else {
		v.drawLabel.SetText(i18n.T("dropdown-battery-draw"))
		v.capacityLabel.SetText(i18n.T("dropdown-battery-capacity"))
	}
	v.setVariant(v.healthDot, healthClass(dev.Capacity), config.TokenFgSubtle)
	v.healthText.SetText(healthValue(dev.Capacity))

	v.chargeCard.SetVisible(dev.ChargeThresholdSupported)
	v.chargeUnsupported.SetVisible(!dev.ChargeThresholdSupported)
	v.chargeTitle.SetText(i18n.T("dropdown-battery-limit-to",
		i18n.Str("threshold", strconv.FormatUint(uint64(dev.ChargeEndThreshold), 10))))
	v.chargeSubtitle.SetText(i18n.T("dropdown-battery-resumes-at",
		i18n.Str("threshold", strconv.FormatUint(uint64(resumeThreshold(dev.ChargeEndThreshold)), 10))))
	v.syncing = true
	v.chargeSwitch.SetOn(dev.ChargeThresholdEnabled)
	v.syncing = false
}

// chargeToggled is handle_charge_limit_toggled: the write runs off the
// loop and the section re-reads after it.
func (v *batteryView) chargeToggled(enabled bool) {
	if v.syncing || v.ctx.Battery == nil {
		return
	}
	src := v.ctx.Battery
	go func() {
		ctx := context.Background()
		if err := src.EnableChargeThreshold(ctx, enabled); err != nil {
			log.Printf("battery: charge threshold toggle: %v", err)
		}
		dev, ok := v.readDevice(ctx)
		v.ctx.Invoke(func() { v.applyDevice(dev, ok) })
	}()
}

// readProfiles reads the daemon; nil when it is absent, so every
// segment disables (ServiceUnavailable).
func (v *batteryView) readProfiles(ctx context.Context) *powerprofiles.Snapshot {
	if v.ctx.PowerProfiles == nil {
		return nil
	}
	snap, err := v.ctx.PowerProfiles.Read(ctx)
	if err != nil || !snap.Available {
		return nil
	}
	return &snap
}

// applyProfiles marks the active segment and disables the ones the
// daemon does not offer; none offered shows the note.
func (v *batteryView) applyProfiles(snap *powerprofiles.Snapshot) {
	var available []string
	v.activeProfile = powerprofiles.ProfileBalanced
	if snap != nil {
		available = snap.Profiles
		v.activeProfile = snap.Active
	}
	for _, p := range batteryProfiles {
		has := slices.Contains(available, p.name)
		v.profileButtons[p.name].SetEnabled(has)
		v.markProfile(p.name, has && p.name == v.activeProfile)
	}
	v.profilesUnavailable.SetVisible(len(available) == 0)
}

// markProfile styles a segment as the toggled one.
func (v *batteryView) markProfile(name string, active bool) {
	b := v.profileButtons[name]
	if active {
		b.AddClass("active")
		b.Bg = tokenColor(v.ctx.Style.palette, config.TokenBgSelected)
	} else {
		b.RemoveClass("active")
		b.Bg = 0
	}
}

// selectProfile is select_profile: the segment toggles at once, the
// write runs off the loop, and the daemon's answer settles it.
func (v *batteryView) selectProfile(name string) {
	src := v.ctx.PowerProfiles
	if src == nil || name == v.activeProfile {
		return
	}
	for _, p := range batteryProfiles {
		v.markProfile(p.name, p.name == name)
	}
	v.activeProfile = name
	go func() {
		ctx := context.Background()
		if err := src.SetActive(ctx, name); err != nil {
			log.Printf("battery: power profile switch: %v", err)
		}
		snap := v.readProfiles(ctx)
		v.ctx.Invoke(func() { v.applyProfiles(snap) })
	}()
}

// follow tracks both services until the dropdown closes.
func (v *batteryView) follow() {
	life, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	if src := v.ctx.Battery; src != nil {
		followTicks(v.ctx, life, "battery", src.Subscribe,
			func(ctx context.Context) batteryRead {
				dev, ok := v.readDevice(ctx)
				return batteryRead{dev, ok}
			},
			func(r batteryRead) { v.applyDevice(r.dev, r.ok) })
	}
	if src := v.ctx.PowerProfiles; src != nil {
		followTicks(v.ctx, life, "power profiles", src.Subscribe, v.readProfiles, v.applyProfiles)
	}
}

// dropdownClosed implements dropdownCloser.
func (v *batteryView) dropdownClosed() { v.once.Do(func() { v.cancel() }) }
