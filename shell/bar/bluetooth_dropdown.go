package bar

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/bluetooth"
)

// The bluetooth dropdown, ported from
// crates/wayle-bar-network/src/dropdowns/bluetooth: the header with the
// scan button and power switch, the pairing card, my devices and the
// available devices, and the empty states.

const (
	btBaseWidth     = 382
	btBaseHeight    = 512
	btScanDuration  = 30 * time.Second
	btActionTimeout = 30 * time.Second
	// btDetailSeparator sits between the device type and the battery.
	btDetailSeparator = "•"
)

// btPending is PendingAction: a device action in flight.
type btPending uint8

const (
	pendingConnecting btPending = iota + 1
	pendingDisconnecting
	pendingForgetting
)

// btDropdown is the live dropdown: it follows the service until its
// popover closes.
type btDropdown struct {
	ctx  ModuleContext
	src  bluetooth.Source
	pal  btPalette
	font render.Font
	px   float64

	root       *btDropdownRoot
	headerIcon *widget.Icon
	scanBtn    *widget.Button
	scanIcon   *widget.Icon
	toggle     *widget.Switch
	card       *btPairingCard

	myLabel, availLabel *widget.Label
	myCard, availCard   *widget.Box
	myList, availList   *widget.Box
	scanningHint        *widget.Label
	emptyNoDevices      widget.Widget
	emptyOff            widget.Widget
	emptyNoAdapter      widget.Widget

	// enabled and available are the dropdown's view, set optimistically
	// by the switch and overwritten when the service's value changes.
	enabled, available bool
	seen               bool
	svcEnabled         bool
	svcAvailable       bool

	scanning bool
	scanGen  int
	pending  map[dbus.ObjectPath]btPending
	// shownPairing is the request the card was last given.
	shownPairing bluetooth.PairingRequest
	myKeys       []string
	availKeys    []string
	stop         func()
}

// btDropdownRoot is the popover content; it releases the subscription
// when the popover closes.
type btDropdownRoot struct {
	*btSurface
	d *btDropdown
}

// dropdownClosed implements dropdownCloser.
func (r *btDropdownRoot) dropdownClosed() { r.d.close() }

// bluetoothDropdown builds the dropdown content; the registry's
// "bluetooth" builder.
func bluetoothDropdown(ctx ModuleContext) widget.Widget {
	if ctx.Bluetooth == nil {
		font, px := dropdownFont(ctx)
		return emptyState(font, px, "ld-bluetooth-off-symbolic",
			btText("dropdown-bluetooth-no-adapter-title"), btText("dropdown-bluetooth-no-adapter-description"))
	}
	d := newBtDropdown(ctx, ctx.Bluetooth)
	ticks, stop := ctx.Bluetooth.Subscribe()
	d.stop = stop
	btDropdownOpened(ctx.Bluetooth)
	go func() {
		for range ticks {
			ctx.Invoke(d.sync)
		}
	}()
	return d.root
}

func newBtDropdown(ctx ModuleContext, src bluetooth.Source) *btDropdown {
	font, px := dropdownFont(ctx)
	d := &btDropdown{
		ctx: ctx, src: src, pal: newBtPalette(ctx), font: font, px: px,
		pending: make(map[dbus.ObjectPath]btPending),
	}
	// The header (DropdownHeader): icon, title, scan button, switch. The
	// template's `.dropdown-title` box lets the stylesheet ink the icon
	// and title.
	d.scanIcon = widget.NewThemeIcon("tb-refresh-symbolic", int(px))
	d.scanBtn = ghostButton(d.scanIcon)
	d.scanBtn.AddClass("ghost-icon", "bluetooth-scan-btn")
	d.scanBtn.OnClick = d.onScan
	d.toggle = widget.NewSwitch(false)
	d.toggle.OnChanged = d.onToggle
	header, glyph, _ := dropdownHeaderParts(font, px, "ld-bluetooth-symbolic", btText("dropdown-bluetooth-title"), d.scanBtn, d.toggle)
	d.headerIcon = glyph

	// The content (DropdownContent + the scrolled column).
	col := widget.NewBox(widget.Column, 8, 12)
	col.AddClass("dropdown-content", "bluetooth-content")
	d.card = newBtPairingCard(ctx)
	d.card.onOutput = d.onPairingOutput
	col.Append(d.card.root, false)

	sectionLabel := func(key string) *widget.Label {
		l := widget.NewLabel(font, px*0.9, btText(key), 0)
		l.AddClass("section-label")
		return l
	}
	d.myLabel = sectionLabel("dropdown-bluetooth-my-devices")
	col.Append(d.myLabel, false)
	d.myList = widget.NewBox(widget.Column, 0, 0)
	d.myCard = widget.NewBox(widget.Column, 0, 0)
	d.myCard.AddClass("card", "bluetooth-device-list")
	col.Append(d.myCard, false)
	d.availLabel = sectionLabel("dropdown-bluetooth-available-devices")
	col.Append(d.availLabel, false)
	d.availList = widget.NewBox(widget.Column, 0, 0)
	d.availCard = widget.NewBox(widget.Column, 0, 0)
	d.availCard.AddClass("card", "bluetooth-device-list")
	col.Append(d.availCard, false)

	d.scanningHint = widget.NewLabel(font, px, btText("dropdown-bluetooth-no-new"), 0)
	d.scanningHint.AddClass("bluetooth-no-new-devices")
	col.Append(d.scanningHint, false)
	d.emptyNoDevices = emptyState(font, px, "ld-bluetooth-searching-symbolic",
		btText("dropdown-bluetooth-no-devices-title"), btText("dropdown-bluetooth-no-devices-description"))
	col.Append(d.emptyNoDevices, false)
	d.emptyOff = emptyState(font, px, "ld-bluetooth-off-symbolic",
		btText("dropdown-bluetooth-off-title"), btText("dropdown-bluetooth-off-description"))
	col.Append(d.emptyOff, false)
	d.emptyNoAdapter = emptyState(font, px, "ld-bluetooth-off-symbolic",
		btText("dropdown-bluetooth-no-adapter-title"), btText("dropdown-bluetooth-no-adapter-description"))
	col.Append(d.emptyNoAdapter, false)

	scroll := dropdownScroll(col, "bluetooth-scroll")
	frame := widget.NewBox(widget.Column, 0, 0)
	frame.AddClass("dropdown", "bluetooth-dropdown")
	frame.Append(header, false)
	frame.Append(scroll, true)
	surface := newBtSurface(frame, 0, 0)
	surface.w, surface.h = btBaseWidth, btBaseHeight
	d.root = &btDropdownRoot{btSurface: surface, d: d}
	d.sync()
	return d
}

// close releases the subscription (the popover went away).
func (d *btDropdown) close() {
	if d.stop != nil {
		d.stop()
		d.stop = nil
		btDropdownClosed(d.src)
	}
}

// async runs work off the loop and then back on it; headless (no
// application) it runs inline so tests are deterministic.
func (d *btDropdown) async(work func(), then func()) {
	if d.ctx.App == nil {
		work()
		if then != nil {
			then()
		}
		return
	}
	go func() {
		work()
		if then != nil {
			d.ctx.Invoke(then)
		}
	}()
}

// sync folds the service state into the view (the update_cmd arms).
func (d *btDropdown) sync() {
	st := d.src.State()
	if !d.seen || st.Enabled != d.svcEnabled {
		d.enabled = st.Enabled
	}
	if !d.seen || st.Available != d.svcAvailable {
		d.available = st.Available
	}
	d.svcEnabled, d.svcAvailable, d.seen = st.Enabled, st.Available, true

	if st.Pairing != d.shownPairing {
		d.showPairing(st)
	}
	mine, avail := splitDeviceLists(st.Devices)
	d.settlePending(mine, avail)
	d.myKeys = d.rebuildList(d.myList, mine, d.myKeys)
	d.availKeys = d.rebuildList(d.availList, avail, d.availKeys)
	d.render()
}

// showPairing is handle_pairing_request: the card shows the request
// for the device it names.
func (d *btDropdown) showPairing(st bluetooth.State) {
	d.shownPairing = st.Pairing
	if st.Pairing == nil {
		d.card.clear()
		return
	}
	display := unknownDisplay
	if dev, ok := st.Device(st.Pairing.DevicePath()); ok {
		display = resolveDeviceDisplay(dev)
	}
	d.card.set(st.Pairing, display)
}

// settlePending drops the pending marks the new state completed
// (update_from_snapshot); a forget completes by the row vanishing.
func (d *btDropdown) settlePending(lists ...[]deviceSnapshot) {
	present := map[dbus.ObjectPath]deviceSnapshot{}
	for _, list := range lists {
		for _, s := range list {
			present[s.path] = s
		}
	}
	for path, action := range d.pending {
		s, ok := present[path]
		switch {
		case !ok:
			delete(d.pending, path)
		case action == pendingConnecting && s.connected, action == pendingDisconnecting && !s.connected:
			delete(d.pending, path)
		}
	}
}

// rebuildList re-renders a list when a row's view changed (the Rust
// reconcile keeps rows; here unchanged lists keep theirs, so RSSI churn
// does not reset a hovered row).
func (d *btDropdown) rebuildList(list *widget.Box, snaps []deviceSnapshot, old []string) []string {
	keys := make([]string, len(snaps))
	for i, s := range snaps {
		keys[i] = fmt.Sprintf("%s|%s|%s|%s|%v|%v|%v|%d|%d", s.path, s.name, s.icon, s.typeKey,
			batteryText(s.battery), s.connected, s.paired, s.category, d.pending[s.path])
	}
	if slices.Equal(keys, old) {
		return old
	}
	list.Clear()
	for _, s := range snaps {
		list.Append(d.buildRow(s), false)
	}
	list.InvalidateLayout()
	return keys
}

func batteryText(battery *uint8) string {
	if battery == nil {
		return ""
	}
	return btText("dropdown-bluetooth-battery", "percent", *battery)
}

// buildRow is DeviceItem's view for one snapshot.
func (d *btDropdown) buildRow(s deviceSnapshot) *btDeviceRow {
	pal, font, px := d.pal, d.font, d.px
	pending := d.pending[s.path]
	row := &btDeviceRow{box: widget.NewBox(widget.Row, 8, 8), hoverSwaps: s.category != categoryAvailable, pending: pending != 0}
	row.box.AddClass("bluetooth-device")
	if s.category == categoryAvailable {
		row.box.AddClass("available")
		row.hoverBg = pal.hover
	}
	if pending != 0 {
		row.box.AddClass("pending")
	}
	row.onClick = func() { d.onDeviceClick(s) }

	icon := widget.NewThemeIcon(s.icon, int(px*1.4))
	// The icon tile (device_item's icon_container): the box paints the
	// well (overlay, or accent-subtle while connected), the image the ink.
	well := iconTile(icon, "bluetooth-device-icon", "bluetooth-icon")
	switch s.category {
	case categoryConnected:
		well.AddClass("connected")
	case categoryPaired:
		well.AddClass("paired")
	}
	row.box.Append(well, false)

	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("bluetooth-device-info")
	name := widget.NewLabel(font, px, s.name, 0)
	name.SetEllipsize(widget.EllipsizeEnd)
	name.AddClass("bluetooth-device-name")
	info.Append(name, false)
	detail := widget.NewBox(widget.Row, 4, 0)
	detail.AddClass("bluetooth-device-detail-row")
	typeLabel := widget.NewLabel(font, px*0.9, btText(s.typeKey), 0)
	typeLabel.AddClass("bluetooth-device-detail")
	detail.Append(typeLabel, false)
	if s.battery != nil {
		sep := widget.NewLabel(font, px*0.9, btDetailSeparator, 0)
		sep.AddClass("bluetooth-detail-separator")
		detail.Append(sep, false)
		bat := widget.NewThemeIcon(batteryLevelIcon(*s.battery), int(px))
		bat.AddClass("bluetooth-battery-icon")
		detail.Append(bat, false)
		pct := widget.NewLabel(font, px*0.9, batteryText(s.battery), 0)
		pct.AddClass("bluetooth-device-detail")
		detail.Append(pct, false)
	}
	info.Append(detail, false)
	row.box.Append(info, true)

	// The trailing slot: status, or the actions while hovered.
	if s.category != categoryAvailable || pending != 0 {
		status := widget.NewLabel(font, px*0.85, statusLabel(s, pending), 0)
		status.AddClass("bluetooth-device-status")
		if pending != 0 {
			status.AddClass("pending")
		}
		row.statusShown = s.connected || s.paired || pending != 0
		row.status = status
		statusPage := widget.NewBox(widget.Row, 0, 0)
		statusPage.Append(status, false)

		actions := widget.NewBox(widget.Row, 4, 0)
		actions.AddClass("bluetooth-device-actions")
		toggleKey := "dropdown-bluetooth-connect"
		if s.connected {
			toggleKey = "dropdown-bluetooth-disconnect"
		}
		toggle := &btActionButton{Button: ghostButton(widget.NewLabel(font, px*0.85, btText(toggleKey), 0)), row: row}
		toggle.AddClass("bluetooth-action-toggle")
		toggle.OnClick = func() { d.onDeviceClick(s) }
		forget := &btActionButton{Button: ghostButton(widget.NewLabel(font, px*0.85, btText("dropdown-bluetooth-forget"), 0)), row: row}
		forget.AddClass("bluetooth-forget")
		forget.OnClick = func() { d.onForget(s) }
		toggle.SetEnabled(pending == 0)
		forget.SetEnabled(pending == 0)
		actions.Append(toggle, false)
		actions.Append(forget, false)
		row.actions = actions
		// The hover stack crossfades between them (HOVER_TRANSITION_MS),
		// as wide as the wider page so hovering never shifts the row.
		row.slot = widget.NewStack()
		row.slot.AddClass("bluetooth-hover-stack")
		row.slot.SetTransition(widget.StackCrossfade, hoverTransition)
		row.slot.Add(btSlotStatus, statusPage)
		row.slot.Add(btSlotActions, actions)
		row.box.Append(row.slot, false)
		row.syncSlot()
	}
	return row
}

// statusLabel is status_label: the pending action, else connected or
// paired.
func statusLabel(s deviceSnapshot, pending btPending) string {
	switch pending {
	case pendingConnecting:
		return btText("dropdown-bluetooth-status-connecting")
	case pendingDisconnecting:
		return btText("dropdown-bluetooth-status-disconnecting")
	case pendingForgetting:
		return btText("dropdown-bluetooth-status-forgetting")
	}
	switch {
	case s.connected:
		return btText("dropdown-bluetooth-connected")
	case s.paired:
		return btText("dropdown-bluetooth-paired")
	}
	return ""
}

// render applies the view's visibility rules (the #[watch] bindings).
func (d *btDropdown) render() {
	icon := "ld-bluetooth-off-symbolic"
	if d.enabled {
		icon = "ld-bluetooth-symbolic"
	}
	d.headerIcon.SetThemeName(icon)
	d.scanBtn.SetVisible(d.available && d.enabled)
	d.scanBtn.SetEnabled(!d.scanning)
	if d.scanning {
		d.scanBtn.AddClass("scanning")
	} else {
		d.scanBtn.RemoveClass("scanning")
	}
	// set_active under block_signal: a state sync is not a user toggle.
	onChanged := d.toggle.OnChanged
	d.toggle.OnChanged = nil
	d.toggle.SetOn(d.enabled)
	d.toggle.OnChanged = onChanged
	d.toggle.SetVisible(d.available)

	haveMine, haveAvail := len(d.myKeys) > 0, len(d.availKeys) > 0
	d.myLabel.SetVisible(d.enabled && haveMine)
	d.myCard.SetVisible(d.enabled && haveMine)
	d.availLabel.SetVisible(d.enabled && (haveAvail || d.scanning))
	d.availCard.SetVisible(d.enabled && haveAvail)
	none := !haveMine && !haveAvail
	d.scanningHint.SetVisible(d.enabled && d.scanning && none)
	setVisible(d.emptyNoDevices, d.enabled && !d.scanning && none)
	setVisible(d.emptyOff, !d.enabled && d.available)
	setVisible(d.emptyNoAdapter, !d.available)
}

func setVisible(w widget.Widget, on bool) {
	if v, ok := w.(interface{ SetVisible(bool) }); ok {
		v.SetVisible(on)
	}
}

// onToggle is handle_bluetooth_toggled.
func (d *btDropdown) onToggle(active bool) {
	d.enabled = active
	if !active {
		d.scanGen++
		d.scanning = false
	}
	d.render()
	d.async(func() {
		var err error
		if active {
			err = d.src.Enable(context.Background())
		} else {
			err = d.src.Disable(context.Background())
		}
		if err != nil {
			log.Printf("bluetooth toggle failed: %v", err)
		}
	}, nil)
}

// onScan is handle_scan_requested: a timed discovery, with the button
// held in its scanning state for the scan's length.
func (d *btDropdown) onScan() {
	if d.scanning {
		return
	}
	d.scanning = true
	d.scanGen++
	gen := d.scanGen
	d.render()
	d.async(func() {
		if err := d.src.StartTimedDiscovery(context.Background(), btScanDuration); err != nil {
			log.Printf("bluetooth scan failed: %v", err)
		}
	}, nil)
	if d.ctx.App != nil {
		time.AfterFunc(btScanDuration, func() { d.ctx.Invoke(func() { d.finishScan(gen) }) })
	}
}

// finishScan is ScanComplete for the scan gen started; a toggle-off in
// between already ended it.
func (d *btDropdown) finishScan(gen int) {
	if gen != d.scanGen {
		return
	}
	d.scanning = false
	d.render()
}

// onDeviceClick is DeviceItem::handle_click: disconnect a connected
// device, connect (and so pair) any other.
func (d *btDropdown) onDeviceClick(s deviceSnapshot) {
	if d.pending[s.path] != 0 {
		return
	}
	if s.connected {
		d.startAction(s.path, pendingDisconnecting, func(ctx context.Context) error {
			return d.src.Disconnect(ctx, s.path)
		})
		return
	}
	d.startAction(s.path, pendingConnecting, func(ctx context.Context) error {
		if err := d.src.Connect(ctx, s.path); err != nil {
			return err
		}
		// BlueZ re-asks the agent to authorize every service on each
		// reconnect of an untrusted device; connecting is consent, so
		// persist it as trust.
		trustPaired(ctx, d.src, s.path)
		return nil
	})
}

// onForget is DeviceItem::handle_forget.
func (d *btDropdown) onForget(s deviceSnapshot) {
	if d.pending[s.path] != 0 {
		return
	}
	d.startAction(s.path, pendingForgetting, func(ctx context.Context) error {
		return d.src.Forget(ctx, s.path)
	})
}

// startAction is handle_device_action: mark the row pending, run the
// action under ACTION_TIMEOUT, and clear the mark on failure.
func (d *btDropdown) startAction(path dbus.ObjectPath, action btPending, run func(context.Context) error) {
	d.pending[path] = action
	d.sync()
	var err error
	d.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), btActionTimeout)
		defer cancel()
		err = run(ctx)
	}, func() {
		if err != nil {
			log.Printf("bluetooth device action failed: %v", err)
			delete(d.pending, path)
			d.sync()
		}
	})
}

// trustPaired is trust_paired_device.
func trustPaired(ctx context.Context, src bluetooth.Source, path dbus.ObjectPath) {
	if err := src.TrustPaired(ctx, path); err != nil {
		log.Printf("bluetooth device trust failed: %v", err)
	}
}

// onPairingOutput is handle_pairing_output. The answers go to the
// service on the loop (they only hand the value to the waiting agent
// call), so the next sync sees the request gone; a refused answer
// leaves the request pending and the card shows it again.
func (d *btDropdown) onPairingOutput(out pairingOutput) {
	d.card.clear()
	d.shownPairing = nil
	if out.kind == outputCancelled {
		d.src.CancelPendingRequest()
		d.sync()
		return
	}
	// Captured before answering: the answer clears the request.
	var serviceAuthPath dbus.ObjectPath
	if out.kind == outputServiceAuthorizationAccepted {
		if req := d.src.State().Pairing; req != nil {
			serviceAuthPath = req.DevicePath()
		}
	}
	if err := provide(d.src, out); err != nil {
		log.Printf("pairing response failed: %v", err)
		d.sync()
		return
	}
	d.sync()
	// Allowing a service is consent for the device: persist it as trust
	// so BlueZ stops re-prompting on every reconnect.
	if serviceAuthPath != "" {
		d.async(func() { trustPaired(context.Background(), d.src, serviceAuthPath) }, nil)
	}
}

// provide routes one card answer to its service provider.
func provide(src bluetooth.Source, out pairingOutput) error {
	switch out.kind {
	case outputLegacyPinSubmitted:
		return src.ProvidePin(out.pin)
	case outputPasskeySubmitted:
		return src.ProvidePasskey(out.passkey)
	case outputPasskeyConfirmed, outputPasskeyRejected:
		return src.ProvideConfirmation(out.kind == outputPasskeyConfirmed)
	case outputAuthorizationAccepted, outputAuthorizationRejected:
		return src.ProvideAuthorization(out.kind == outputAuthorizationAccepted)
	case outputServiceAuthorizationAccepted, outputServiceAuthorizationRejected:
		return src.ProvideServiceAuthorization(out.kind == outputServiceAuthorizationAccepted)
	}
	return nil
}
