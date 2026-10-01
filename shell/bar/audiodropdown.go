package bar

import (
	"context"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/service/pulse"
)

// PulseAudio property keys the dropdown reads (helpers.rs).
const (
	paStreamRestoreID  = "module-stream-restore.id"
	paRoleEvent        = "sink-input-by-media-role:event"
	paAppIconName      = "application.icon_name"
	paAppName          = "application.name"
	paAppProcessBinary = "application.process.binary"
	paFormFactor       = "device.form_factor"
)

// isEventStream is is_event_stream: event sounds have no app row.
func isEventStream(props map[string]string) bool {
	return props[paStreamRestoreID] == paRoleEvent
}

// streamIcon is stream_icon: Mapped tries the curated map on the app
// name, binary, then icon name, falling back to the icon name made
// symbolic; Native is the icon name, else the binary. "" is none.
func streamIcon(props map[string]string, source config.AppIconSource) string {
	if source == config.AppIconSourceNative {
		if name := props[paAppIconName]; name != "" {
			return name
		}
		return props[paAppProcessBinary]
	}
	for _, key := range []string{paAppName, paAppProcessBinary, paAppIconName} {
		if value, ok := props[key]; ok {
			if icon, ok := appicons.Lookup(value); ok {
				return icon
			}
		}
	}
	name, ok := props[paAppIconName]
	if !ok {
		return ""
	}
	if strings.HasSuffix(name, "-symbolic") {
		return name
	}
	return name + "-symbolic"
}

// resolveStreamIcon is resolve_stream_icon: a Native icon the theme
// lacks falls back to the Mapped one.
func resolveStreamIcon(props map[string]string, source config.AppIconSource, exists func(string) bool) string {
	icon := streamIcon(props, source)
	if source == config.AppIconSourceNative && (icon == "" || !exists(icon)) {
		return streamIcon(props, config.AppIconSourceMapped)
	}
	return icon
}

// audioVolumeIcon is helpers.rs's volume_icon.
func audioVolumeIcon(percentage float64, muted bool) string {
	switch {
	case muted || percentage <= 0:
		return "ld-volume-x-symbolic"
	case percentage < 34:
		return "ld-volume-symbolic"
	case percentage < 67:
		return "ld-volume-1-symbolic"
	}
	return "ld-volume-2-symbolic"
}

// audioInputIcon is input_icon.
func audioInputIcon(muted bool) string {
	if muted {
		return "ld-mic-off-symbolic"
	}
	return "ld-mic-symbolic"
}

// appDisplayName is app_display_name: the application name, else the
// stream name, first rune uppercased.
func appDisplayName(applicationName, streamName string) string {
	name := applicationName
	if name == "" {
		name = streamName
	}
	if name == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// outputDeviceIcon is output_device_icon: the form factor, else a guess
// from the name and description.
func outputDeviceIcon(name, description string, props map[string]string) string {
	switch props[paFormFactor] {
	case "internal", "speaker", "hifi":
		return "tb-device-speaker-symbolic"
	case "headphone":
		return "tb-headphones-symbolic"
	case "headset":
		return "tb-headset-symbolic"
	case "phone":
		return "tb-device-mobile-symbolic"
	case "portable":
		return "tb-radio-symbolic"
	case "car":
		return "tb-car-symbolic"
	case "computer":
		return "tb-device-desktop-symbolic"
	}
	haystack := strings.ToLower(name + " " + description)
	switch {
	case strings.Contains(haystack, "hdmi"), strings.Contains(haystack, "displayport"), strings.Contains(haystack, "monitor"):
		return "tb-device-tv-symbolic"
	case strings.Contains(haystack, "headset"), strings.Contains(haystack, "airpods"):
		return "tb-headset-symbolic"
	case strings.Contains(haystack, "headphone"), strings.Contains(haystack, "bluetooth"):
		return "tb-headphones-symbolic"
	}
	return "tb-device-speaker-symbolic"
}

// inputDeviceIcon is input_device_icon.
func inputDeviceIcon(name, description string, props map[string]string) string {
	switch props[paFormFactor] {
	case "internal", "microphone":
		return "tb-microphone-symbolic"
	case "headset":
		return "tb-headset-symbolic"
	case "webcam":
		return "tb-device-computer-camera-symbolic"
	case "phone":
		return "tb-device-mobile-symbolic"
	}
	haystack := strings.ToLower(name + " " + description)
	switch {
	case strings.Contains(haystack, "headset"), strings.Contains(haystack, "airpods"):
		return "tb-headset-symbolic"
	case strings.Contains(haystack, "bluetooth"):
		return "tb-headphones-symbolic"
	case strings.Contains(haystack, "webcam"), strings.Contains(haystack, "camera"):
		return "tb-device-computer-camera-symbolic"
	}
	return "tb-microphone-symbolic"
}

// activePortDescription is active_port_description; "" when the device
// has no active port or it is not listed.
func activePortDescription(active string, ports []pulse.DevicePort) string {
	if active == "" {
		return ""
	}
	for _, p := range ports {
		if p.Name == active {
			return p.Description
		}
	}
	return ""
}

// audioKind is VolumeSectionKind.
type audioKind int

const (
	audioOutput audioKind = iota
	audioInput
)

// audioDevice is one device as a section or picker row shows it.
type audioDevice struct {
	key         pulse.DeviceKey
	description string
	subtitle    string
	icon        string
	volume      pulse.Volume
	muted       bool
}

// audioState is one read of the service: the physical devices of each
// kind (monitors dropped from the inputs), the defaults, and the
// playback streams.
type audioState struct {
	outputs, inputs       []audioDevice
	defaultOut, defaultIn *audioDevice
	streams               []pulse.AudioStream
}

func outputEntry(d pulse.Device) audioDevice {
	return audioDevice{
		d.Key, d.Description, activePortDescription(d.ActivePort, d.Ports),
		outputDeviceIcon(d.Name, d.Description, d.Properties), d.Volume, d.Muted,
	}
}

func inputEntry(d pulse.Device) audioDevice {
	return audioDevice{
		d.Key, d.Description, activePortDescription(d.ActivePort, d.Ports),
		inputDeviceIcon(d.Name, d.Description, d.Properties), d.Volume, d.Muted,
	}
}

// readAudioState snapshots src.
func readAudioState(ctx context.Context, src pulse.Source) audioState {
	var s audioState
	for _, d := range src.OutputDevices() {
		s.outputs = append(s.outputs, outputEntry(d.Device))
	}
	for _, d := range src.InputDevices() {
		if !d.IsMonitor() {
			s.inputs = append(s.inputs, inputEntry(d.Device))
		}
	}
	if d, err := src.DefaultSink(ctx); err == nil {
		e := outputEntry(d.Device)
		s.defaultOut = &e
	}
	if d, err := src.DefaultSource(ctx); err == nil && !d.IsMonitor() {
		e := inputEntry(d.Device)
		s.defaultIn = &e
	}
	s.streams = src.PlaybackStreams()
	return s
}

// audioView is the audio dropdown: the main page (the default output
// and input sections over the application volumes, or the empty state)
// and a device picker page per kind.
type audioView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	pages *widget.Stack
	main  *widget.Stack

	output, input *volumeSection
	apps          *appVolumes
	pickers       [2]*devicePicker

	once   sync.Once
	cancel context.CancelFunc
}

func audioDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &audioView{ctx: ctx, font: font, px: px, cancel: func() {}}
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "audio-dropdown")
	v.Append(dropdownHeader(ctx, font, px, "ld-volume-2-symbolic", i18n.T("dropdown-audio-title")), false)

	v.output = newVolumeSection(v, audioOutput)
	v.input = newVolumeSection(v, audioInput)
	v.apps = newAppVolumes(v)
	content := widget.NewBox(widget.Column, 10, 0)
	devices := widget.NewBox(widget.Column, 10, 0)
	devices.AddClass("audio-fixed", "audio-devices")
	devices.Append(v.output, false)
	devices.Append(v.input, false)
	content.Append(devices, false)
	appLabel := widget.NewLabel(font, px*0.85, i18n.T("dropdown-audio-app-volume"), mutedFg(ctx.Style.palette))
	appLabel.AddClass("section-label")
	content.Append(appLabel, false)
	content.Append(v.apps, true)
	v.main = widget.NewStack()
	v.main.Add("devices", content)
	v.main.Add("empty", emptyState(ctx, font, px, "ld-volume-x-symbolic",
		i18n.T("dropdown-audio-no-devices-title"), i18n.T("dropdown-audio-no-devices-description")))

	v.pickers[audioOutput] = newDevicePicker(v, audioOutput, i18n.T("dropdown-audio-output-devices"))
	v.pickers[audioInput] = newDevicePicker(v, audioInput, i18n.T("dropdown-audio-input-devices"))
	v.pages = widget.NewStack()
	v.pages.AddClass("dropdown-content")
	v.pages.Add("main", v.main)
	v.pages.Add("output", v.pickers[audioOutput])
	v.pages.Add("input", v.pickers[audioInput])
	v.pages.Show("main")
	v.Append(v.pages, true)

	if ctx.Pulse != nil {
		v.apply(readAudioState(context.Background(), ctx.Pulse))
		v.follow()
	} else {
		v.apply(audioState{})
	}
	return v
}

// apply pushes one state into every part.
func (v *audioView) apply(s audioState) {
	v.output.apply(s.defaultOut, len(s.outputs) > 0)
	v.input.apply(s.defaultIn, len(s.inputs) > 0)
	if len(s.outputs) > 0 || len(s.inputs) > 0 {
		v.main.Show("devices")
	} else {
		v.main.Show("empty")
	}
	v.apps.apply(s.streams)
	v.pickers[audioOutput].apply(s.outputs, s.defaultOut)
	v.pickers[audioInput].apply(s.inputs, s.defaultIn)
}

// showPage switches the page stack ("main", "output", "input").
func (v *audioView) showPage(name string) { v.pages.Show(name) }

// write runs a service control off the loop, as the Rust command
// futures do; the follow loop picks up the result.
func (v *audioView) write(what string, fn func(context.Context, pulse.Source) error) {
	src := v.ctx.Pulse
	if src == nil {
		return
	}
	go func() {
		if err := fn(context.Background(), src); err != nil {
			log.Printf("audio: %s: %v", what, err)
		}
	}()
}

func (v *audioView) follow() {
	life, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	src := v.ctx.Pulse
	followTicks(v.ctx, life, "audio", src.Subscribe,
		func(ctx context.Context) audioState { return readAudioState(ctx, src) }, v.apply)
}

// dropdownClosed implements dropdownCloser.
func (v *audioView) dropdownClosed() { v.once.Do(func() { v.cancel() }) }

// volumeSection is VolumeSection: the kind's label, the default
// device's icon and name (opening the picker), its mute button and
// labeled slider, or the no-device row.
type volumeSection struct {
	*widget.Box
	view *audioView
	kind audioKind

	trigger, sliderRow, noDevice *widget.Box
	icon, muteIcon               *widget.Icon
	name                         *widget.Label
	mute                         *widget.Button
	slider                       *debouncedSlider

	device *audioDevice
}

func newVolumeSection(v *audioView, kind audioKind) *volumeSection {
	s := &volumeSection{view: v, kind: kind, Box: widget.NewBox(widget.Column, 6, 0)}
	s.AddClass("audio-device")
	fg := v.ctx.Style.fg

	s.trigger = widget.NewBox(widget.Row, 10, 0)
	s.trigger.AddClass("audio-device-trigger")
	s.icon = widget.NewThemeIcon("", int(v.px*1.4))
	s.icon.SetTint(fg)
	s.icon.AddClass("audio-device-icon")
	s.trigger.Append(s.icon, false)
	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("audio-device-info")
	title := i18n.T("dropdown-audio-output")
	page := "output"
	if kind == audioInput {
		title, page = i18n.T("dropdown-audio-input"), "input"
	}
	label := widget.NewLabel(v.font, v.px*0.85, title, mutedFg(v.ctx.Style.palette))
	label.AddClass("audio-device-label")
	info.Append(label, false)
	nameRow := widget.NewBox(widget.Row, 4, 0)
	nameRow.AddClass("audio-device-name")
	s.name = widget.NewLabel(v.font, v.px, "", fg)
	s.name.AddClass("audio-device-name-text")
	s.name.SetEllipsize(widget.EllipsizeEnd)
	nameRow.Append(s.name, true)
	chevron := widget.NewThemeIcon("ld-chevron-right-symbolic", int(v.px*0.9))
	chevron.SetTint(mutedFg(v.ctx.Style.palette))
	chevron.AddClass("audio-device-chevron")
	nameRow.Append(chevron, false)
	info.Append(dropdownButton(v.ctx, nameRow, "audio-device-trigger-btn", func() { v.showPage(page) }), false)
	s.trigger.Append(info, true)
	s.muteIcon = widget.NewThemeIcon("", int(v.px*1.1))
	s.muteIcon.SetTint(fg)
	s.muteIcon.AddClass("audio-mute-icon")
	s.mute = dropdownButton(v.ctx, s.muteIcon, "audio-mute-btn", s.toggleMute)
	s.trigger.Append(s.mute, false)
	s.Append(s.trigger, false)

	s.slider = newDebouncedSlider(0, v.font, v.px*0.9, fg, v.ctx.Invoke)
	s.slider.knob.AddClass("audio-volume-slider")
	s.slider.onCommit = s.commitVolume
	s.slider.onValue = func(float64) { s.syncMuteIcon() }
	s.sliderRow = widget.NewBox(widget.Row, 0, 0)
	s.sliderRow.AddClass("audio-slider-row")
	s.sliderRow.Append(s.slider, true)
	s.Append(s.sliderRow, false)

	s.noDevice = widget.NewBox(widget.Row, 6, 0)
	s.noDevice.AddClass("audio-no-device")
	alert := widget.NewThemeIcon("tb-alert-triangle-symbolic", int(v.px))
	alert.SetTint(mutedFg(v.ctx.Style.palette))
	s.noDevice.Append(widget.NewSpacer(0, 0), true)
	s.noDevice.Append(alert, false)
	s.noDevice.Append(widget.NewLabel(v.font, v.px*0.9, i18n.T("dropdown-audio-no-device"), mutedFg(v.ctx.Style.palette)), false)
	s.noDevice.Append(widget.NewSpacer(0, 0), true)
	s.Append(s.noDevice, false)
	return s
}

// apply is DeviceChanged plus VolumeOrMuteChanged: has decides the
// trigger or the no-device row; the default, when there is one, syncs
// the name, icon, slider, and mute state.
func (s *volumeSection) apply(dev *audioDevice, has bool) {
	s.trigger.SetVisible(has)
	s.sliderRow.SetVisible(has)
	s.noDevice.SetVisible(!has)
	if dev == nil {
		return
	}
	s.device = dev
	s.name.SetText(dev.description)
	s.icon.SetThemeName(dev.icon)
	s.slider.set(dev.volume.AveragePercentage())
	s.setMuted(dev.muted)
}

func (s *volumeSection) setMuted(muted bool) {
	if muted {
		s.AddClass("muted")
		s.mute.AddClass("muted")
	} else {
		s.RemoveClass("muted")
		s.mute.RemoveClass("muted")
	}
	s.syncMuteIcon()
}

// syncMuteIcon is mute_icon.
func (s *volumeSection) syncMuteIcon() {
	muted := s.device != nil && s.device.muted
	if s.kind == audioInput {
		s.muteIcon.SetThemeName(audioInputIcon(muted))
		return
	}
	s.muteIcon.SetThemeName(audioVolumeIcon(s.slider.value(), muted))
}

// commitVolume is commit_volume.
func (s *volumeSection) commitVolume(pct float64) {
	if s.device == nil {
		return
	}
	key, volume := s.device.key, pulse.VolumeFromPercentage(pct, s.device.volume.Channels())
	s.view.write("set volume", func(ctx context.Context, src pulse.Source) error {
		return src.SetDeviceVolume(ctx, key, volume)
	})
}

// toggleMute is toggle_mute.
func (s *volumeSection) toggleMute() {
	if s.device == nil {
		return
	}
	key, muted := s.device.key, !s.device.muted
	s.view.write("toggle mute", func(ctx context.Context, src pulse.Source) error {
		return src.SetDeviceMute(ctx, key, muted)
	})
}

// appVolume is one AppVolumeItem row.
type appVolume struct {
	key   pulse.StreamKey
	name  string
	icon  string
	vol   pulse.Volume
	muted bool
}

// appVolumeRows is sync_app_volumes's filter: no event streams, one row
// per process, sorted by name.
func appVolumeRows(streams []pulse.AudioStream, source config.AppIconSource, exists func(string) bool) []appVolume {
	seen := map[uint32]bool{}
	var rows []appVolume
	for _, st := range streams {
		if isEventStream(st.Properties) {
			continue
		}
		if st.PID != 0 {
			if seen[st.PID] {
				continue
			}
			seen[st.PID] = true
		}
		rows = append(rows, appVolume{
			key:   st.Key,
			name:  appDisplayName(st.ApplicationName, st.Name),
			icon:  resolveStreamIcon(st.Properties, source, exists),
			vol:   st.Volume,
			muted: st.Muted,
		})
	}
	slices.SortStableFunc(rows, func(a, b appVolume) int { return strings.Compare(a.name, b.name) })
	return rows
}

// appVolumes is AppVolumes: the scrolled rows or the no-apps state.
type appVolumes struct {
	*widget.Stack
	view  *audioView
	list  *widget.Box
	rows  []appVolume
	items map[pulse.StreamKey]*appVolumeItem
}

func newAppVolumes(v *audioView) *appVolumes {
	a := &appVolumes{view: v, Stack: widget.NewStack()}
	a.list = widget.NewBox(widget.Column, 8, 0)
	a.list.AddClass("audio-app-list")
	scroll := dropdownScroll(a.list, "app-volumes-scroll")
	a.Add("apps", scroll)
	a.Add("empty", emptyState(v.ctx, v.font, v.px, "ld-volume-x-symbolic", i18n.T("dropdown-audio-no-apps"), ""))
	return a
}

// apply rebuilds when the rows' identity changed (the stream list),
// otherwise pushes each stream's volume and mute into its row
// (sync_single_app_volume).
func (a *appVolumes) apply(streams []pulse.AudioStream) {
	rows := appVolumeRows(streams, a.view.ctx.Config.Volume.DropdownAppIcons, widget.ThemeIconExists)
	same := a.items != nil && slices.EqualFunc(rows, a.rows, func(x, y appVolume) bool {
		return x.key == y.key && x.name == y.name && x.icon == y.icon
	})
	a.rows = rows
	if same {
		for _, r := range rows {
			a.items[r.key].apply(r)
		}
		return
	}
	a.list.Clear()
	a.items = make(map[pulse.StreamKey]*appVolumeItem, len(rows))
	for _, r := range rows {
		item := newAppVolumeItem(a.view, r)
		a.items[r.key] = item
		a.list.Append(item, false)
	}
	if len(rows) == 0 {
		a.Show("empty")
	} else {
		a.Show("apps")
	}
}

// appVolumeItem is AppVolumeItem: icon, name, value, mute, slider.
type appVolumeItem struct {
	*widget.Box
	view     *audioView
	row      appVolume
	value    *widget.Label
	mute     *widget.Button
	muteIcon *widget.Icon
	slider   *debouncedSlider
}

func newAppVolumeItem(v *audioView, r appVolume) *appVolumeItem {
	it := &appVolumeItem{view: v, row: r, Box: widget.NewBox(widget.Column, 4, 0)}
	it.AddClass("audio-app-item")
	fg := v.ctx.Style.fg
	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("audio-app-header")
	iconName := r.icon
	if iconName == "" {
		iconName = "ld-app-window-symbolic"
	}
	icon := widget.NewThemeIcon(iconName, int(v.px*1.2))
	icon.SetTint(fg)
	icon.AddClass("audio-app-icon")
	header.Append(icon, false)
	name := widget.NewLabel(v.font, v.px, r.name, fg)
	name.AddClass("audio-app-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	header.Append(name, true)
	it.value = widget.NewLabel(v.font, v.px*0.9, "", mutedFg(v.ctx.Style.palette))
	it.value.AddClass("audio-app-value")
	header.Append(it.value, false)
	it.muteIcon = widget.NewThemeIcon("", int(v.px))
	it.muteIcon.SetTint(fg)
	it.muteIcon.AddClass("audio-mute-icon")
	it.mute = dropdownButton(v.ctx, it.muteIcon, "audio-mute-btn", it.toggleMute)
	header.Append(it.mute, false)
	it.Append(header, false)

	it.slider = newDebouncedSlider(r.vol.AveragePercentage(), nil, 0, fg, v.ctx.Invoke)
	it.slider.AddClass("audio-app-slider")
	it.slider.knob.AddClass("audio-app-scale")
	it.slider.onCommit = it.commitVolume
	it.slider.onValue = func(float64) { it.sync() }
	it.Append(it.slider, false)
	it.sync()
	return it
}

// apply is SetBackendState.
func (it *appVolumeItem) apply(r appVolume) {
	it.row = r
	it.slider.set(r.vol.AveragePercentage())
	it.sync()
}

// sync refreshes the #[watch]ed value label, mute icon, and classes.
func (it *appVolumeItem) sync() {
	pct := it.slider.value()
	it.value.SetText(strconv.FormatFloat(pct, 'f', 0, 64) + "%")
	it.muteIcon.SetThemeName(audioVolumeIcon(pct, it.row.muted))
	if it.row.muted {
		it.AddClass("audio-muted")
		it.mute.AddClass("muted")
	} else {
		it.RemoveClass("audio-muted")
		it.mute.RemoveClass("muted")
	}
}

// commitVolume is commit_app_volume.
func (it *appVolumeItem) commitVolume(pct float64) {
	key, volume := it.row.key, pulse.VolumeFromPercentage(pct, it.row.vol.Channels())
	it.view.write("set app volume", func(ctx context.Context, src pulse.Source) error {
		return src.SetStreamVolume(ctx, key, volume)
	})
}

// toggleMute is toggle_app_mute.
func (it *appVolumeItem) toggleMute() {
	key, muted := it.row.key, !it.row.muted
	it.view.write("toggle app mute", func(ctx context.Context, src pulse.Source) error {
		return src.SetStreamMute(ctx, key, muted)
	})
}

// devicePicker is DevicePicker: the back button and title over one row
// per physical device of the kind, the default checked.
type devicePicker struct {
	*widget.Box
	view *audioView
	kind audioKind
	list *widget.Box
}

func newDevicePicker(v *audioView, kind audioKind, title string) *devicePicker {
	p := &devicePicker{view: v, kind: kind, Box: widget.NewBox(widget.Column, 8, 0)}
	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("picker-header")
	back := widget.NewThemeIcon("ld-arrow-left-symbolic", int(v.px))
	back.SetTint(v.ctx.Style.fg)
	header.Append(dropdownButton(v.ctx, back, "picker-back", func() { v.showPage("main") }), false)
	titleLabel := widget.NewLabel(v.font, v.px*1.05, title, v.ctx.Style.fg)
	titleLabel.AddClass("picker-title")
	header.Append(titleLabel, true)
	p.Append(header, false)
	p.list = widget.NewBox(widget.Column, 4, 0)
	p.list.AddClass("audio-device-list")
	scroll := dropdownScroll(p.list, "picker-body")
	p.Append(scroll, true)
	return p
}

// apply is build_*_device_list into apply_device_list.
func (p *devicePicker) apply(devices []audioDevice, def *audioDevice) {
	p.list.Clear()
	for _, d := range devices {
		p.list.Append(p.row(d, def != nil && def.key == d.key), false)
	}
}

// row is DeviceOptionItem; a click returns to the main page and makes
// the device the default (select_device).
func (p *devicePicker) row(d audioDevice, active bool) widget.Widget {
	v := p.view
	content := widget.NewBox(widget.Row, 10, 0)
	content.AddClass("audio-device-option-content")
	icon := widget.NewThemeIcon(d.icon, int(v.px*1.2))
	icon.SetTint(v.ctx.Style.fg)
	icon.AddClass("audio-device-option-icon")
	content.Append(icon, false)
	info := widget.NewBox(widget.Column, 2, 0)
	name := widget.NewLabel(v.font, v.px, d.description, v.ctx.Style.fg)
	name.AddClass("audio-device-option-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	info.Append(name, false)
	if d.subtitle != "" {
		sub := widget.NewLabel(v.font, v.px*0.85, d.subtitle, mutedFg(v.ctx.Style.palette))
		sub.AddClass("audio-device-option-subtitle")
		sub.SetEllipsize(widget.EllipsizeEnd)
		info.Append(sub, false)
	}
	content.Append(info, true)
	if active {
		check := widget.NewThemeIcon("tb-check-symbolic", int(v.px))
		check.SetTint(tokenColor(v.ctx.Style.palette, config.TokenAccent))
		check.AddClass("audio-device-option-check")
		content.Append(check, false)
	}
	key := d.key
	b := dropdownButton(v.ctx, content, "audio-device-option", func() {
		v.showPage("main")
		v.write("set default device", func(ctx context.Context, src pulse.Source) error {
			return src.SetDefault(ctx, key)
		})
	})
	if active {
		b.AddClass("selected")
		b.Bg = tokenColor(v.ctx.Style.palette, config.TokenBgSelected)
	}
	return b
}
