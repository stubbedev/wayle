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
	"github.com/stubbedev/wayle/shell/widgets"
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
	v.Box = widget.NewBox(widget.Column, 0, 14)
	v.AddClass("dropdown", "audio-dropdown")
	v.Append(dropdownHeader(font, px, "ld-volume-2-symbolic", i18n.T("dropdown-audio-title")), false)

	v.output = newVolumeSection(v, audioOutput)
	v.input = newVolumeSection(v, audioInput)
	v.apps = newAppVolumes(v)
	content := widget.NewBox(widget.Column, 0, 0)
	devices := widget.NewBox(widget.Column, 0, 0)
	devices.AddClass("audio-fixed", "audio-devices")
	devices.Append(v.output, false)
	devices.Append(v.input, false)
	content.Append(devices, false)
	appLabel := widget.NewLabel(font, px*0.85, i18n.T("dropdown-audio-app-volume"), 0)
	appLabel.AddClass("section-label")
	content.Append(appLabel, false)
	content.Append(v.apps, true)
	v.main = widget.NewStack()
	v.main.Add("devices", content)
	v.main.Add("empty", centeredEmpty(emptyState(font, px, "ld-volume-x-symbolic", i18n.T("dropdown-audio-no-devices-title"), i18n.T("dropdown-audio-no-devices-description"))))

	v.pickers[audioOutput] = newDevicePicker(v, audioOutput, i18n.T("dropdown-audio-output-devices"))
	v.pickers[audioInput] = newDevicePicker(v, audioInput, i18n.T("dropdown-audio-input-devices"))
	v.pages = widget.NewStack()
	v.pages.AddClass("dropdown-content")
	pageSlide(v.pages, ctx.Config)
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

// centeredEmpty wraps an empty state the way its Rust callers do (the
// vexpand, valign-Center boxes of main_section/mod.rs:58 and
// app_volumes/mod.rs:56): the stack page keeps the full height and the
// state sits centered in it.
func centeredEmpty(col *widget.Box) *widget.Box {
	wrap := widget.NewBox(widget.Row, 0, 0)
	wrap.AppendAligned(col, true, widget.AlignCenter)
	return wrap
}

// volumeSection is VolumeSection: the kind's label, the default
// device's icon and name (opening the picker), its mute button and
// labeled slider, or the no-device row.
type volumeSection struct {
	*widget.Box
	view *audioView
	kind audioKind

	trigger, nameRow, sliderRow, noDevice *widget.Box
	icon, muteIcon                        *widget.Icon
	name                                  *widget.Label
	mute                                  *widget.Button
	slider                                *widgets.DebouncedSlider

	device *audioDevice
}

func newVolumeSection(v *audioView, kind audioKind) *volumeSection {
	s := &volumeSection{view: v, kind: kind, Box: widget.NewBox(widget.Column, 0, 0)}
	s.AddClass("audio-device")

	s.trigger = widget.NewBox(widget.Row, 0, 0)
	s.trigger.AddClass("audio-device-trigger")
	s.icon = widget.NewThemeIcon("", int(v.px*1.4))
	s.trigger.AppendAligned(iconTile(s.icon, "audio-device-icon", "audio-device-icon-img"), false, widget.AlignCenter)
	info := widget.NewBox(widget.Column, 0, 0)
	info.AddClass("audio-device-info")
	title := i18n.T("dropdown-audio-output")
	page := "output"
	if kind == audioInput {
		title, page = i18n.T("dropdown-audio-input"), "input"
	}
	label := widget.NewLabel(v.font, v.px*0.85, title, 0)
	label.AddClass("audio-device-label")
	info.Append(label, false)
	nameRow := widget.NewBox(widget.Row, 0, 0)
	nameRow.AddClass("audio-device-name")
	s.nameRow = nameRow
	s.name = widget.NewLabel(v.font, v.px, "", 0)
	s.name.AddClass("audio-device-name-text")
	s.name.SetEllipsize(widget.EllipsizeEnd)
	nameRow.Append(s.name, true)
	chevron := widget.NewThemeIcon("ld-chevron-right-symbolic", int(v.px*0.9))
	chevron.AddClass("audio-device-chevron")
	nameRow.Append(chevron, false)
	info.Append(dropdownButton(nameRow, "audio-device-trigger-btn", func() { v.showPage(page) }), false)
	s.trigger.Append(info, true)
	s.muteIcon = widget.NewThemeIcon("", int(v.px*1.1))
	s.muteIcon.AddClass("audio-mute-icon")
	s.mute = dropdownButton(s.muteIcon, "audio-mute-btn", s.toggleMute)
	s.trigger.AppendAligned(s.mute, false, widget.AlignCenter)
	s.Append(s.trigger, false)

	s.slider = widgets.NewDebouncedSlider(0, v.font, v.px*0.9, 0, v.ctx.Invoke)
	s.slider.Knob.AddClass("audio-volume-slider")
	s.slider.ValueLabel().AddClass("audio-slider-value")
	s.slider.OnCommit = s.commitVolume
	s.slider.OnValue = func(float64) { s.syncMuteIcon() }
	s.sliderRow = widget.NewBox(widget.Row, 0, 0)
	s.sliderRow.AddClass("audio-slider-row")
	s.sliderRow.Append(s.slider, true)
	s.Append(s.sliderRow, false)

	s.noDevice = widget.NewBox(widget.Row, 0, 0)
	s.noDevice.AddClass("audio-no-device")
	alert := widget.NewThemeIcon("tb-alert-triangle-symbolic", int(v.px))
	alert.AddClass("audio-no-device-icon")
	s.noDevice.Append(alert, false)
	noDeviceLabel := widget.NewLabel(v.font, v.px*0.9, i18n.T("dropdown-audio-no-device"), 0)
	noDeviceLabel.AddClass("audio-no-device-label")
	s.noDevice.Append(noDeviceLabel, false)
	// set_halign Center (volume_section/mod.rs:121); the icon rule's
	// margin-right spaces it from the label.
	s.AppendAligned(s.noDevice, false, widget.AlignCenter)
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
	s.slider.Set(dev.volume.AveragePercentage())
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
	s.muteIcon.SetThemeName(audioVolumeIcon(s.slider.Value(), muted))
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
	a.list = widget.NewBox(widget.Column, 0, 0)
	a.list.AddClass("audio-app-list")
	inner := widget.NewBox(widget.Column, 0, 0)
	inner.AddClass("app-volumes-inner")
	inner.Append(a.list, false)
	scroll := dropdownScroll(inner, "app-volumes-scroll")
	a.Add("apps", scroll)
	// The no-apps state's icon carries the sm size modifier
	// (app_volumes/mod.rs:66).
	empty, glyph := emptyStateIcon(v.font, v.px, "ld-volume-x-symbolic", i18n.T("dropdown-audio-no-apps"), "")
	glyph.AddClass("sm")
	a.Add("empty", centeredEmpty(empty))
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
	slider   *widgets.DebouncedSlider
}

func newAppVolumeItem(v *audioView, r appVolume) *appVolumeItem {
	it := &appVolumeItem{view: v, row: r, Box: widget.NewBox(widget.Column, 0, 0)}
	it.AddClass("audio-app-item")
	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("audio-app-header")
	iconName := r.icon
	if iconName == "" {
		iconName = "ld-app-window-symbolic"
	}
	icon := widget.NewThemeIcon(iconName, int(v.px*1.2))
	header.AppendAligned(iconTile(icon, "audio-app-icon", "audio-app-icon-img"), false, widget.AlignCenter)
	name := widget.NewLabel(v.font, v.px, r.name, 0)
	name.AddClass("audio-app-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	header.Append(name, true)
	it.value = widget.NewLabel(v.font, v.px*0.9, "", 0)
	it.value.AddClass("audio-app-value")
	header.Append(it.value, false)
	it.muteIcon = widget.NewThemeIcon("", int(v.px))
	it.muteIcon.AddClass("audio-mute-icon")
	it.mute = dropdownButton(it.muteIcon, "audio-mute-btn", it.toggleMute)
	header.AppendAligned(it.mute, false, widget.AlignCenter)
	it.Append(header, false)

	it.slider = widgets.NewDebouncedSlider(r.vol.AveragePercentage(), nil, 0, 0, v.ctx.Invoke)
	it.slider.AddClass("audio-app-slider")
	it.slider.Knob.AddClass("audio-app-scale")
	it.slider.OnCommit = it.commitVolume
	it.slider.OnValue = func(float64) { it.sync() }
	it.Append(it.slider, false)
	it.sync()
	return it
}

// apply is SetBackendState.
func (it *appVolumeItem) apply(r appVolume) {
	it.row = r
	it.slider.Set(r.vol.AveragePercentage())
	it.sync()
}

// sync refreshes the #[watch]ed value label, mute icon, and classes.
func (it *appVolumeItem) sync() {
	pct := it.slider.Value()
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
	// built snapshots the rows the list shows and which is the
	// default, so a tick that changed nothing skips the clear+rebuild
	// and the row widgets (their hover state with them) survive.
	built     []audioDevice
	active    pulse.DeviceKey
	hasActive bool
}

func newDevicePicker(v *audioView, kind audioKind, title string) *devicePicker {
	p := &devicePicker{view: v, kind: kind, Box: widget.NewBox(widget.Column, 0, 0)}
	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("picker-header")
	back := widget.NewThemeIcon("ld-arrow-left-symbolic", int(v.px))
	backBtn := dropdownButton(back, "picker-back", func() { v.showPage("main") })
	backBtn.AddClass("ghost-icon") // GhostIconButton's own class (device_picker/mod.rs:41)
	header.Append(backBtn, false)
	titleLabel := widget.NewLabel(v.font, v.px*1.05, title, 0)
	titleLabel.AddClass("picker-title")
	header.Append(titleLabel, false) // natural width (device_picker/mod.rs:47)
	p.Append(header, false)
	p.list = widget.NewBox(widget.Column, 0, 0)
	p.list.AddClass("audio-device-list")
	scroll := dropdownScroll(p.list, "picker-body")
	p.Append(scroll, true)
	return p
}

// apply is apply_device_list: the rows rebuild only when the list or
// the default changed, so no-op ticks keep the row widgets alive.
func (p *devicePicker) apply(devices []audioDevice, def *audioDevice) {
	active, hasActive := pulse.DeviceKey{}, false
	if def != nil {
		active, hasActive = def.key, true
	}
	same := p.built != nil && hasActive == p.hasActive && active == p.active &&
		len(devices) == len(p.built) &&
		slices.EqualFunc(devices, p.built, func(a, b audioDevice) bool {
			return a.key == b.key && a.description == b.description &&
				a.subtitle == b.subtitle && a.icon == b.icon
		})
	if same {
		return
	}
	p.built, p.hasActive, p.active = devices, hasActive, active
	p.list.Clear()
	for _, d := range devices {
		p.list.Append(p.row(d, def != nil && def.key == d.key), false)
	}
}

// optionContent builds the row's content (device_item.rs:34): the
// icon tile as a CenterBox (its CSS class paints it, the image inside
// has none), the name (and subtitle) column, and the active check.
func (p *devicePicker) optionContent(d audioDevice, active bool) *widget.Box {
	v := p.view
	content := widget.NewBox(widget.Row, 0, 0)
	content.AddClass("audio-device-option-content")
	icon := widget.NewThemeIcon(d.icon, int(v.px*1.2))
	tile := widget.NewCenterBox(nil, icon, nil)
	tile.AddClass("audio-device-option-icon")
	content.Append(tile, false)
	info := widget.NewBox(widget.Column, 0, 0)
	name := widget.NewLabel(v.font, v.px, d.description, 0)
	name.AddClass("audio-device-option-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	info.Append(name, false)
	if d.subtitle != "" {
		sub := widget.NewLabel(v.font, v.px*0.85, d.subtitle, 0)
		sub.AddClass("audio-device-option-subtitle")
		sub.SetEllipsize(widget.EllipsizeEnd)
		info.Append(sub, false)
	}
	content.AppendAligned(info, true, widget.AlignCenter)
	if active {
		check := widget.NewThemeIcon("tb-check-symbolic", int(v.px))
		check.AddClass("audio-device-option-check")
		content.Append(check, false)
	}
	return content
}

// row is DeviceOptionItem; a click returns to the main page and makes
// the device the default (select_device).
func (p *devicePicker) row(d audioDevice, active bool) widget.Widget {
	v := p.view
	key := d.key
	b := dropdownButton(p.optionContent(d, active), "audio-device-option", func() {
		v.showPage("main")
		v.write("set default device", func(ctx context.Context, src pulse.Source) error {
			return src.SetDefault(ctx, key)
		})
	})
	if active {
		b.AddClass("selected")
	}
	return b
}
