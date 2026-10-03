package bar

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/desktopnotify"
	"github.com/stubbedev/wayle/service/bluetooth"
)

const (
	pathHeadset = dbus.ObjectPath("/org/bluez/hci0/dev_01")
	pathSpeaker = dbus.ObjectPath("/org/bluez/hci0/dev_02")
	pathMouse   = dbus.ObjectPath("/org/bluez/hci0/dev_03")
)

// btWorld is a powered adapter with a connected headset, a paired
// mouse, and an unpaired speaker nearby.
func btWorld() bluetooth.State {
	battery := uint8(64)
	class := uint32(0x240418)
	return bluetooth.State{
		Available: true, Enabled: true, Primary: &bluetooth.Adapter{Powered: true},
		Devices: []bluetooth.Device{
			{Path: pathSpeaker, Alias: "Speaker", Name: new("Speaker"), Icon: new("audio-speakers")},
			{Path: pathHeadset, Alias: "Headset", Name: new("Headset"), Icon: new("audio-headphones"), Connected: true, Paired: true, BatteryPercentage: &battery},
			{Path: pathMouse, Alias: "Mouse", Name: new("Mouse"), Class: &class, Paired: true},
		},
	}
}

func newTestBtDropdown(t *testing.T, st bluetooth.State) (*btDropdown, *fakeBluetooth) {
	t.Helper()
	src := newFakeBluetooth(st)
	return newBtDropdown(newTestContext(t, config.Defaults()), src), src
}

func visible(w widget.Widget) bool {
	v, ok := w.(interface{ Visible() bool })
	return !ok || v.Visible()
}

func rows(list *widget.Box) []*btDeviceRow {
	var out []*btDeviceRow
	for _, kid := range list.Children() {
		if r, ok := kid.(*btDeviceRow); ok {
			out = append(out, r)
		}
	}
	return out
}

func rowNamed(t *testing.T, d *btDropdown, name string) *btDeviceRow {
	t.Helper()
	for _, list := range []*widget.Box{d.myList, d.availList} {
		for _, r := range rows(list) {
			info := r.Children()[1].(*widget.Box)
			if info.Children()[0].(*widget.Label).Text() == name {
				return r
			}
		}
	}
	t.Fatalf("no row %q", name)
	return nil
}

func TestBtDropdownListsDevices(t *testing.T) {
	d, _ := newTestBtDropdown(t, btWorld())
	mine, avail := rows(d.myList), rows(d.availList)
	if len(mine) != 2 || len(avail) != 1 {
		t.Fatalf("rows mine %d avail %d, want 2 and 1", len(mine), len(avail))
	}
	// Connected sorts before paired.
	if got := mine[0].status.Text(); got != "Connected" {
		t.Errorf("first row status = %q, want the connected headset", got)
	}
	if got := mine[1].status.Text(); got != "Paired" {
		t.Errorf("second row status = %q", got)
	}
	if !visible(d.myLabel) || !visible(d.myCard) || !visible(d.availLabel) || !visible(d.availCard) {
		t.Error("the device sections are hidden")
	}
	for _, empty := range []widget.Widget{d.emptyNoDevices, d.emptyOff, d.emptyNoAdapter, d.scanningHint} {
		if visible(empty) {
			t.Error("an empty state shows beside devices")
		}
	}
	// The battery detail: separator, icon, percent.
	detail := mine[0].Children()[1].(*widget.Box).Children()[1].(*widget.Box)
	if n := len(detail.Children()); n != 4 {
		t.Fatalf("headset detail has %d parts, want type, separator, icon, percent", n)
	}
	if got := plain(detail.Children()[3].(*widget.Label).Text()); got != "64%" {
		t.Errorf("battery = %q", got)
	}
	if got := detail.Children()[0].(*widget.Label).Text(); got != "Headphones" {
		t.Errorf("type = %q", got)
	}
	// Available rows carry no status/actions slot and no hover swap.
	if avail[0].actions != nil || avail[0].hoverSwaps {
		t.Error("an available row has the my-device slot")
	}
}

func TestBtDropdownEmptyStates(t *testing.T) {
	d, src := newTestBtDropdown(t, bluetooth.State{})
	if !visible(d.emptyNoAdapter) || visible(d.toggle) || visible(d.scanBtn) {
		t.Error("no adapter: want the no-adapter state without switch or scan")
	}
	src.setState(bluetooth.State{Available: true, Primary: &bluetooth.Adapter{}})
	d.sync()
	if !visible(d.emptyOff) || visible(d.emptyNoAdapter) || !visible(d.toggle) || visible(d.scanBtn) {
		t.Error("adapter off: want the off state and the switch")
	}
	if d.toggle.On() {
		t.Error("the switch reads on for a powered-off adapter")
	}
	src.setState(btState())
	d.sync()
	if !visible(d.emptyNoDevices) || visible(d.emptyOff) || !visible(d.scanBtn) || !d.toggle.On() {
		t.Error("on with no devices: want the no-devices state and scan")
	}
	if d.headerIcon.Name() != "ld-bluetooth-symbolic" {
		t.Errorf("header icon = %q", d.headerIcon.Name())
	}
	// A state sync does not call the service back through the switch.
	if calls := src.Calls(); len(calls) != 0 {
		t.Errorf("syncs made calls: %v", calls)
	}
}

func TestBtDropdownScan(t *testing.T) {
	d, src := newTestBtDropdown(t, btState())
	d.scanBtn.OnClick()
	if !slices.Equal(src.Calls(), []string{"scan 30s"}) {
		t.Errorf("calls = %v, want a 30s timed discovery", src.Calls())
	}
	if !d.scanning || d.scanBtn.Enabled() || !widget.HasClass(d.scanBtn, "scanning") {
		t.Error("the scan button is not held in its scanning state")
	}
	if !visible(d.scanningHint) || !visible(d.availLabel) || visible(d.emptyNoDevices) {
		t.Error("scanning with no devices: want the no-new hint under the available label")
	}
	// A second press while scanning does nothing.
	d.onScan()
	if len(src.Calls()) != 1 {
		t.Error("a second scan started while scanning")
	}
	d.finishScan(d.scanGen)
	if d.scanning || !d.scanBtn.Enabled() || widget.HasClass(d.scanBtn, "scanning") {
		t.Error("the scan did not complete")
	}
}

func TestBtDropdownToggleOffCancelsTheScan(t *testing.T) {
	d, src := newTestBtDropdown(t, btState())
	d.onScan()
	gen := d.scanGen
	d.toggle.SetOn(false) // the user flips the switch
	if !slices.Contains(src.Calls(), "disable") {
		t.Errorf("calls = %v, want disable", src.Calls())
	}
	if d.scanning || d.enabled {
		t.Error("toggle off left the scan or the enabled view")
	}
	if !visible(d.emptyOff) {
		t.Error("toggle off does not show the off state")
	}
	// A sync with the service still reporting on keeps the switch where
	// the user put it until the service's value changes.
	d.sync()
	if d.toggle.On() {
		t.Error("a sync with an unchanged service value flipped the switch back")
	}
	src.setState(bluetooth.State{Available: true, Primary: &bluetooth.Adapter{}})
	d.sync()
	src.setState(btState())
	d.sync()
	if !d.toggle.On() {
		t.Error("a service change to on does not show")
	}
	// The old scan's timer no longer ends anything.
	d.onScan()
	d.finishScan(gen)
	if !d.scanning {
		t.Error("a stale scan timer ended the new scan")
	}
	d.toggle.SetOn(true)
	if slices.Contains(src.Calls(), "enable") {
		t.Error("setting the switch to its current state called the service")
	}
}

func TestBtDropdownConnectAvailableDevice(t *testing.T) {
	d, src := newTestBtDropdown(t, btWorld())
	rowNamed(t, d, "Speaker").clickRow(t)
	want := []string{"connect " + string(pathSpeaker), "trust-paired " + string(pathSpeaker)}
	if !slices.Equal(src.Calls(), want) {
		t.Fatalf("calls = %v, want %v", src.Calls(), want)
	}
	row := rowNamed(t, d, "Speaker")
	if !row.pending || row.status == nil || row.status.Text() != "Connecting..." || !widget.HasClass(row.Box, "pending") {
		t.Error("a connecting row does not show its pending state")
	}
	// A click while pending does nothing.
	row.clickRow(t)
	if len(src.Calls()) != 2 {
		t.Error("a pending row took a second click")
	}
	// The state catching up settles it.
	st := btWorld()
	st.Devices[0].Connected, st.Devices[0].Paired = true, true
	src.setState(st)
	d.sync()
	if row := rowNamed(t, d, "Speaker"); row.pending || row.status.Text() != "Connected" {
		t.Error("the completed connect is still pending")
	}
}

func TestBtDropdownFailedConnectClearsPending(t *testing.T) {
	d, src := newTestBtDropdown(t, btWorld())
	src.fail["connect"] = errors.New("page timeout")
	rowNamed(t, d, "Speaker").clickRow(t)
	if slices.Contains(src.Calls(), "trust-paired "+string(pathSpeaker)) {
		t.Error("a failed connect still trusted the device")
	}
	if row := rowNamed(t, d, "Speaker"); row.pending {
		t.Error("a failed connect left the row pending")
	}
}

func TestBtDropdownDisconnectAndForget(t *testing.T) {
	d, src := newTestBtDropdown(t, btWorld())
	rowNamed(t, d, "Headset").clickRow(t)
	if got := src.Calls(); len(got) != 1 || got[0] != "disconnect "+string(pathHeadset) {
		t.Fatalf("calls = %v, want a disconnect", got)
	}
	if got := rowNamed(t, d, "Headset").status.Text(); got != "Disconnecting..." {
		t.Errorf("status = %q", got)
	}
	mouse := rowNamed(t, d, "Mouse")
	forget := mouse.actions.Children()[1].(*widget.Button)
	forget.OnClick()
	if got := src.Calls(); len(got) != 2 || got[1] != "forget "+string(pathMouse) {
		t.Fatalf("calls = %v, want a forget", got)
	}
	if got := rowNamed(t, d, "Mouse").status.Text(); got != "Removing..." {
		t.Errorf("status = %q", got)
	}
	// The row vanishing completes the forget.
	st := btWorld()
	st.Devices = st.Devices[:2]
	src.setState(st)
	d.sync()
	if _, ok := d.pending[pathMouse]; ok {
		t.Error("the forgotten device is still pending")
	}
}

func TestBtDeviceRowHoverSwapsStatusForActions(t *testing.T) {
	d, _ := newTestBtDropdown(t, btWorld())
	row := rowNamed(t, d, "Headset")
	if row.slot.Visible() != btSlotStatus || !visible(row.status) {
		t.Fatal("at rest the row shows its status")
	}
	// One pointer: the router moves over the row, onto a button, then
	// off the row entirely.
	row.Measure(widget.Constraints{Max: widget.Size{W: 400, H: 200}})
	row.Arrange(render.Rect{X: 0, Y: 0, W: 400, H: 40})
	router := &widget.Router{Root: row}
	router.Move(widget.Point{X: 10, Y: 10})
	if row.slot.Visible() != btSlotActions || !row.slot.Switching() {
		t.Fatal("hovered, the row crossfades to its actions")
	}
	toggle := row.actions.Children()[0].(*widget.Button)
	b := toggle.Bounds()
	// Moving onto a button keeps the actions: the button is a
	// descendant, so the row's hover-within holds.
	router.Move(widget.Point{X: b.X + b.W/2, Y: b.Y + b.H/2})
	if row.slot.Visible() != btSlotActions {
		t.Error("moving onto an action hid the actions")
	}
	router.Move(widget.Point{X: 1000, Y: 1000})
	if row.slot.Visible() != btSlotStatus || !visible(row.status) {
		t.Error("leaving the row keeps the actions")
	}
	// Hovering an available row never swaps.
	speaker := rowNamed(t, d, "Speaker")
	speaker.Measure(widget.Constraints{Max: widget.Size{W: 400, H: 200}})
	speaker.Arrange(render.Rect{X: 0, Y: 0, W: 400, H: 40})
	(&widget.Router{Root: speaker}).Move(widget.Point{X: 10, Y: 10})
	if speaker.actions != nil {
		t.Error("an available row grew actions")
	}
}

// clickRow presses and releases through a Router, the real input path:
// the hit lands inside the row and the row's SetOnClickWithin hook
// hears it.
func (r *btDeviceRow) clickRow(t *testing.T) {
	t.Helper()
	r.Measure(widget.Constraints{Max: widget.Size{W: 400, H: 200}})
	r.Arrange(render.Rect{X: 0, Y: 0, W: 400, H: 40})
	router := &widget.Router{Root: r}
	p := widget.Point{X: 10, Y: 10}
	router.Move(p)
	router.Press(widget.BTNLeft, p)
	router.Release(widget.BTNLeft, p)
}

func TestBtPairingConfirmation(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestConfirmation{Device: pathHeadset, Passkey: 42}
	d, src := newTestBtDropdown(t, st)
	c := d.card
	if !visible(c.root) || c.variant != variantRequestConfirmation {
		t.Fatal("the card does not show the confirmation")
	}
	if c.deviceName.Text() != "Headset" || c.deviceType.Text() != "Headphones" || c.icon.Name() != "ld-headphones-symbolic" {
		t.Errorf("header = %q %q %q", c.deviceName.Text(), c.deviceType.Text(), c.icon.Name())
	}
	if c.pinCode[1].Text() != "000042" {
		t.Errorf("code = %q, want the zero-padded passkey", c.pinCode[1].Text())
	}
	if c.leftLabel.Text() != "Reject" || c.rightLabel.Text() != "Confirm" || !visible(c.right) {
		t.Errorf("actions = %q/%q", c.leftLabel.Text(), c.rightLabel.Text())
	}
	c.right.OnClick()
	if !slices.Equal(src.Calls(), []string{"confirmation true"}) {
		t.Errorf("calls = %v", src.Calls())
	}
	if visible(c.root) {
		t.Error("an answered card stays")
	}
}

func TestBtPairingRejectAndClose(t *testing.T) {
	for _, c := range []struct {
		req  bluetooth.PairingRequest
		want string
	}{
		{bluetooth.RequestConfirmation{Device: pathHeadset, Passkey: 1}, "confirmation false"},
		{bluetooth.RequestAuthorization{Device: pathHeadset}, "authorization false"},
		{bluetooth.RequestServiceAuthorization{Device: pathHeadset, UUID: "0000110b-0000-1000-8000-00805f9b34fb"}, "service-authorization false"},
		{bluetooth.RequestPasskey{Device: pathHeadset}, "cancel"},
		{bluetooth.RequestPinCode{Device: pathHeadset}, "cancel"},
		{bluetooth.DisplayPinCode{Device: pathHeadset, PinCode: "123456"}, "cancel"},
	} {
		st := btWorld()
		st.Pairing = c.req
		d, src := newTestBtDropdown(t, st)
		// The left action rejects the yes/no prompts, cancels the rest.
		d.card.emit(d.card.rejectOutput())
		if got := src.Calls(); len(got) != 1 || got[0] != c.want {
			t.Errorf("%T reject: calls = %v, want %q", c.req, got, c.want)
		}
		// The close button always cancels.
		d2, src2 := newTestBtDropdown(t, st)
		d2.card.emit(pairingOutput{kind: outputCancelled})
		if got := src2.Calls(); len(got) != 1 || got[0] != "cancel" {
			t.Errorf("%T close: calls = %v", c.req, got)
		}
	}
}

func TestBtPairingPasskeyEntry(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestPasskey{Device: pathSpeaker}
	d, src := newTestBtDropdown(t, st)
	c := d.card
	if c.rightLabel.Text() != "Pair" || c.leftLabel.Text() != "Reject" {
		t.Errorf("actions = %q/%q", c.leftLabel.Text(), c.rightLabel.Text())
	}
	// Nothing typed: nothing to send.
	c.right.OnClick()
	if len(src.Calls()) != 0 {
		t.Errorf("an empty passkey was sent: %v", src.Calls())
	}
	// Six single-digit boxes, digits only.
	if len(c.pinDigits) != passkeyTotal {
		t.Fatalf("digit boxes = %d, want %d", len(c.pinDigits), passkeyTotal)
	}
	c.pinDigits[0].SetText("12a3456789")
	if got := c.pinDigits[0].Text(); got != "1" {
		t.Errorf("a digit box took %q, want one digit", got)
	}
	for i, digit := range []string{"2", "3", "4", "5", "6"} {
		c.pinDigits[i+1].SetText(digit)
	}
	if got := c.passkeyText(); got != "123456" {
		t.Errorf("passkey = %q, want the six digits", got)
	}
	c.right.OnClick()
	if !slices.Equal(src.Calls(), []string{"passkey 123456"}) {
		t.Errorf("calls = %v", src.Calls())
	}
	// A partial passkey still submits, the Rust build_confirm_output's
	// PinSubmitted(passkey) over whatever the boxes spell — its provider
	// does the refusing.
	st.Pairing = bluetooth.RequestPasskey{Device: pathSpeaker}
	src.setState(st)
	d.sync()
	c = d.card
	c.pinDigits[0].SetText("9")
	c.right.OnClick()
	if !slices.Contains(src.Calls(), "passkey 9") {
		t.Errorf("a partial passkey is not submitted: %v", src.Calls())
	}
}

func TestBtPairingLegacyPin(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestPinCode{Device: pathSpeaker}
	d, src := newTestBtDropdown(t, st)
	c := d.card
	if c.legacyPinEntry.Placeholder() != "PIN" {
		t.Errorf("placeholder = %q", c.legacyPinEntry.Placeholder())
	}
	c.legacyPinEntry.SetText("00000000000000000000")
	if got := c.legacyPinEntry.Text(); len(got) != 16 {
		t.Errorf("entry = %q, want 16 characters at most", got)
	}
	c.legacyPinEntry.SetText("0000")
	c.right.OnClick()
	if !slices.Equal(src.Calls(), []string{"pin 0000"}) {
		t.Errorf("calls = %v", src.Calls())
	}
}

func TestBtPairingServiceAuthorizationTrusts(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestServiceAuthorization{Device: pathHeadset, UUID: "0000111e-0000-1000-8000-00805f9b34fb"}
	d, src := newTestBtDropdown(t, st)
	if got := d.card.serviceName.Text(); got != "Hands-Free" {
		t.Errorf("service = %q", got)
	}
	if d.card.rightLabel.Text() != "Allow" || d.card.leftLabel.Text() != "Deny" {
		t.Error("service authorization labels")
	}
	d.card.right.OnClick()
	want := []string{"service-authorization true", "trust-paired " + string(pathHeadset)}
	if !slices.Equal(src.Calls(), want) {
		t.Errorf("calls = %v, want %v", src.Calls(), want)
	}
}

func TestBtPairingAuthorizationAllowDoesNotTrust(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestAuthorization{Device: pathSpeaker}
	d, src := newTestBtDropdown(t, st)
	d.card.right.OnClick()
	if !slices.Equal(src.Calls(), []string{"authorization true"}) {
		t.Errorf("calls = %v", src.Calls())
	}
}

func TestBtPairingDisplayRequests(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.DisplayPasskey{Device: pathHeadset, Passkey: 7, Entered: 2}
	d, src := newTestBtDropdown(t, st)
	c := d.card
	if visible(c.right) || c.leftLabel.Text() != "Cancel" {
		t.Error("a display request offers a confirm")
	}
	if plain(c.progress.Text()) != "2 of 6 digits entered" || c.pinCode[2].Text() != "000007" {
		t.Errorf("progress %q code %q", c.progress.Text(), c.pinCode[2].Text())
	}
	// The typing progress updates in place.
	st.Pairing = bluetooth.DisplayPasskey{Device: pathHeadset, Passkey: 7, Entered: 5}
	src.setState(st)
	d.sync()
	if plain(c.progress.Text()) != "5 of 6 digits entered" {
		t.Errorf("progress = %q", c.progress.Text())
	}
	// BlueZ withdrawing it clears the card.
	st.Pairing = nil
	src.setState(st)
	d.sync()
	if visible(c.root) {
		t.Error("a withdrawn request stays on the card")
	}
}

func TestBtPairingUnknownDevice(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestAuthorization{Device: "/org/bluez/hci0/dev_NEW"}
	d, _ := newTestBtDropdown(t, st)
	if d.card.deviceName.Text() != "-" || d.card.deviceType.Text() != "Bluetooth device" {
		t.Errorf("unknown device header = %q %q", d.card.deviceName.Text(), d.card.deviceType.Text())
	}
}

func TestBtPairingRefusedAnswerShowsTheRequestAgain(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestPinCode{Device: pathSpeaker}
	d, src := newTestBtDropdown(t, st)
	src.fail["pin"] = errors.New("bluetooth: cannot provide pin: 0 characters")
	d.card.right.OnClick()
	if !visible(d.card.root) || d.card.variant != variantRequestPinCode {
		t.Error("a refused answer hid the still-pending request")
	}
}

func TestBtDropdownClosedStopsFollowing(t *testing.T) {
	src := newFakeBluetooth(btWorld())
	ctx := newTestContext(t, config.Defaults())
	ctx.Bluetooth = src
	content := bluetoothDropdown(ctx)
	closer, ok := content.(dropdownCloser)
	if !ok {
		t.Fatal("the bluetooth dropdown does not release its subscription")
	}
	if !btDropdownVisible(src) {
		t.Error("an open dropdown reads as closed")
	}
	closer.dropdownClosed()
	if btDropdownVisible(src) {
		t.Error("a closed dropdown reads as open")
	}
	// Closing twice does not underflow.
	closer.dropdownClosed()
	btDropdownOpened(src)
	if !btDropdownVisible(src) {
		t.Error("the open count underflowed")
	}
	btDropdownClosed(src)
}

func TestBtDropdownWithoutServiceShowsNoAdapter(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	content := bluetoothDropdown(ctx)
	// The full shell builds without a service; closing it is a no-op,
	// not a panic, and nothing subscribed.
	if c, ok := content.(dropdownCloser); ok {
		c.dropdownClosed()
	}
	if findBox(content, "dropdown-header") == nil {
		t.Error("the no-adapter dropdown dropped the header")
	}
}

// fakeNotifier records notifications and answers Ask with key.
type fakeNotifier struct {
	sent, asked []string
	key         string
	answered    bool
}

func (f *fakeNotifier) Send(_ context.Context, _, summary, body, _ string) (uint32, error) {
	f.sent = append(f.sent, summary+": "+body)
	return 1, nil
}

func (f *fakeNotifier) Ask(_ context.Context, _, _, body, _ string, actions []desktopnotify.Action, timeout time.Duration) (string, bool, error) {
	f.asked = append(f.asked, body)
	if len(actions) != 2 || actions[0].Key != "deny" || actions[1].Key != "allow" || timeout != 60*time.Second {
		return "", false, errors.New("unexpected actions")
	}
	return f.key, f.answered, nil
}

func newTestNotifier(st bluetooth.State, key string, answered bool) (*btPairingNotifier, *fakeBluetooth, *fakeNotifier) {
	src := newFakeBluetooth(st)
	sender := &fakeNotifier{key: key, answered: answered}
	return &btPairingNotifier{src: src, sender: sender, spawn: func(fn func()) { fn() }}, src, sender
}

func TestBtNotifierAsksForConfirmation(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestConfirmation{Device: pathHeadset, Passkey: 99}
	n, src, sender := newTestNotifier(st, "allow", true)
	n.check()
	want := "Headset wants to pair with passkey 000099 — open the Bluetooth menu to respond"
	if len(sender.asked) != 1 || plain(sender.asked[0]) != want {
		t.Fatalf("asked = %v", sender.asked)
	}
	wantCalls := []string{"confirmation true", "trust-paired " + string(pathHeadset)}
	if !slices.Equal(src.Calls(), wantCalls) {
		t.Errorf("calls = %v, want %v", src.Calls(), wantCalls)
	}
	// The same request does not notify twice.
	n.check()
	if len(sender.asked) != 1 {
		t.Error("an unchanged request notified again")
	}
}

func TestBtNotifierDenyDoesNotTrust(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestServiceAuthorization{Device: pathHeadset, UUID: "x"}
	n, src, _ := newTestNotifier(st, "deny", true)
	n.check()
	if !slices.Equal(src.Calls(), []string{"service-authorization false"}) {
		t.Errorf("calls = %v", src.Calls())
	}
}

func TestBtNotifierIgnoredNotificationAnswersNothing(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestAuthorization{Device: pathSpeaker}
	n, src, sender := newTestNotifier(st, "", false)
	n.check()
	if len(sender.asked) != 1 || len(src.Calls()) != 0 {
		t.Errorf("asked %v calls %v, want one ask and no answer", sender.asked, src.Calls())
	}
}

func TestBtNotifierLinksInputPrompts(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestPasskey{Device: "/org/bluez/hci0/dev_NEW"}
	n, src, sender := newTestNotifier(st, "allow", true)
	n.check()
	if len(sender.asked) != 0 || len(sender.sent) != 1 {
		t.Fatalf("sent %v asked %v, want one plain notification", sender.sent, sender.asked)
	}
	if plain(sender.sent[0]) != "Bluetooth pairing request: New device wants to connect — open the Bluetooth menu to respond" {
		t.Errorf("notification = %q", sender.sent[0])
	}
	if len(src.Calls()) != 0 {
		t.Error("a link-only notification answered the agent")
	}
}

func TestBtNotifierQuietWhileTheDropdownIsOpen(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestAuthorization{Device: pathSpeaker}
	n, src, sender := newTestNotifier(st, "allow", true)
	btDropdownOpened(src)
	defer btDropdownClosed(src)
	n.check()
	if len(sender.asked)+len(sender.sent) != 0 {
		t.Error("notified while the dropdown shows the request")
	}
}

// findBox walks the tree for the first box carrying class.
func findBox(w widget.Widget, class string) *widget.Box {
	var found *widget.Box
	walkTree(w, func(k widget.Widget) bool {
		if found != nil {
			return false
		}
		if b, ok := k.(*widget.Box); ok && widget.HasClass(b, class) {
			found = b
		}
		return found == nil
	})
	return found
}

// findButton walks the tree for the first button carrying class.
func findButton(w widget.Widget, class string) *widget.Button {
	var found *widget.Button
	walkTree(w, func(k widget.Widget) bool {
		if found != nil {
			return false
		}
		if b, ok := k.(*widget.Button); ok && widget.HasClass(b, class) {
			found = b
		}
		return found == nil
	})
	return found
}

// The no-adapter state is the whole dropdown shell — the header stays
// above the empty state (mod.rs:73-147,303-326); the early return of a
// bare empty state is the regression.
func TestBtNoAdapterDropdownKeepsTheHeader(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	content := bluetoothDropdown(ctx)
	if c, ok := content.(dropdownCloser); ok {
		c.dropdownClosed()
	}
	if findBox(content, "dropdown-header") == nil {
		t.Error("the no-adapter dropdown dropped the header")
	}
	empty := findBox(content, "empty-state")
	if empty == nil {
		t.Fatal("the no-adapter dropdown lost its empty state")
	}
	if !visible(empty) {
		t.Error("the no-adapter empty state is hidden")
	}
	// The shell is the dropdown frame at its fixed size, not a bare box.
	if findBox(content, "bluetooth-dropdown") == nil {
		t.Fatal("the no-adapter state is not the dropdown frame")
	}
	sz := content.Measure(widget.Constraints{Max: widget.Size{W: 500, H: 700}})
	if sz.W != btBaseWidth || sz.H != btBaseHeight {
		t.Errorf("size = %dx%d, want the %dx%d base", sz.W, sz.H, btBaseWidth, btBaseHeight)
	}
	// A live dropdown with an adapter keeps the same header; the
	// no-adapter state never grows device rows.
	d, _ := newTestBtDropdown(t, btWorld())
	if findBox(d.root, "dropdown-header") == nil {
		t.Error("the live dropdown lost its header")
	}
	if findBox(content, "bluetooth-device") != nil {
		t.Error("the no-adapter state shows a device row")
	}
}

// The passkey prompt is six single-digit boxes (pairing_card/mod.rs:
// 147-190), classed entry.bluetooth-pin-digit, in a centered row that
// only the RequestPasskey variant shows.
func TestBtPairingCardDigitBoxes(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestPasskey{Device: pathSpeaker}
	d, src := newTestBtDropdown(t, st)
	c := d.card
	if len(c.pinDigits) != passkeyTotal {
		t.Fatalf("digit boxes = %d, want %d", len(c.pinDigits), passkeyTotal)
	}
	for i, digit := range c.pinDigits {
		if !digit.HasClass("bluetooth-pin-digit") {
			t.Errorf("digit box %d lost bluetooth-pin-digit", i)
		}
		if _, maxChars := digit.WidthChars(); maxChars != 1 {
			t.Errorf("digit box %d max width = %d chars, want 1", i, maxChars)
		}
	}
	if !visible(c.pinRow) || len(c.pinRow.Children()) != passkeyTotal {
		t.Error("the pin row is not the six boxes")
	}
	// The legacy PIN box is not a digit box, and the row hides for the
	// other variants.
	if c.legacyPinEntry.HasClass("bluetooth-pin-digit") {
		t.Error("the legacy PIN entry wears the digit class")
	}
	st.Pairing = bluetooth.RequestConfirmation{Device: pathSpeaker, Passkey: 1}
	src.setState(st)
	d.sync()
	if visible(c.pinRow) {
		t.Error("the digit boxes show for a confirmation prompt")
	}
	st.Pairing = bluetooth.RequestPasskey{Device: pathSpeaker}
	src.setState(st)
	d.sync()
	if !visible(c.pinRow) {
		t.Error("the digit boxes stayed hidden for a passkey prompt")
	}
}

// fluent renders placeables wrapped in Unicode isolation marks (the
// Rust fluent-rs defaults to isolating): tests compare the plain text.
func plain(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\u2068' || r == '\u2069' {
			return -1
		}
		return r
	}, s)
}

// The progress dots box rides in the display-passkey code block
// (pairing_card/mod.rs:229-230): an empty box the stylesheet spaces,
// with no dot children of its own.
func TestBtPairingCardProgressDots(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.DisplayPasskey{Device: pathHeadset, Passkey: 7, Entered: 2}
	d, _ := newTestBtDropdown(t, st)
	dots := findBox(d.card.root, "bluetooth-progress-dots")
	if dots == nil {
		t.Fatal("no bluetooth-progress-dots box")
	}
	if n := len(dots.Children()); n != 0 {
		t.Errorf("the dots box has %d children, want none (the Rust box is empty)", n)
	}
}

// Ghost-icon buttons carry ghost-icon only; ghost buttons carry ghost
// only — the two templates do not mix (mod.rs:95-121,
// pairing_card/mod.rs:94-100).
func TestBtGhostClassesAreExact(t *testing.T) {
	d, _ := newTestBtDropdown(t, btWorld())
	if !widget.HasClass(d.scanBtn, "ghost-icon") || !widget.HasClass(d.scanBtn, "bluetooth-scan-btn") {
		t.Errorf("scan button classes wrong: ghost-icon %v scan-btn %v",
			widget.HasClass(d.scanBtn, "ghost-icon"), widget.HasClass(d.scanBtn, "bluetooth-scan-btn"))
	}
	if widget.HasClass(d.scanBtn, "ghost") {
		t.Error("the scan button carries the ghost class")
	}
	closeBtn := findButton(d.card.root, "bluetooth-pairing-close")
	if closeBtn == nil {
		t.Fatal("no pairing close button")
	}
	if widget.HasClass(closeBtn, "ghost") || !widget.HasClass(closeBtn, "ghost-icon") {
		t.Error("the close button is not ghost-icon only")
	}
	// The row actions are GhostButtons: ghost, not ghost-icon.
	mouse := rowNamed(t, d, "Mouse")
	forget := mouse.actions.Children()[1].(*widget.Button)
	if !widget.HasClass(forget, "ghost") || !widget.HasClass(forget, "bluetooth-forget") {
		t.Error("the forget button is not the ghost template")
	}
	if widget.HasClass(forget, "ghost-icon") {
		t.Error("the forget button carries the ghost-icon class")
	}
}

// A typed digit steps the keyboard to the next box (handle_pin_key's
// grab_focus), through the popover handle.
func TestBtPairingDigitBoxesStepTheFocus(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestPasskey{Device: pathSpeaker}
	d, _ := newTestBtDropdown(t, st)
	pop := &fakePopover{}
	d.root.attachPopover(pop)
	c := d.card
	c.pinDigits[2].SetText("4")
	if pop.focused != c.pinDigits[3] {
		t.Error("typing a digit did not move to the next box")
	}
	// The last box has no next.
	pop.focused = nil
	c.pinDigits[5].SetText("1")
	if pop.focused != nil {
		t.Error("the last digit box stepped past itself")
	}
	// A filtered-out keystroke does not step.
	pop.focused = nil
	c.pinDigits[0].SetText("")
	c.pinDigits[0].SetText("x")
	if pop.focused != nil {
		t.Error("a rejected keystroke stepped the focus")
	}
}

// Backspace on the digit row is the Rust key controller
// (handle_pin_key): on an empty box it clears and focuses the previous
// one, on a filled box it clears it, and the entry never sees it.
func TestBtPairingPinBackspaceSteps(t *testing.T) {
	st := btWorld()
	st.Pairing = bluetooth.RequestPasskey{Device: pathSpeaker}
	d, _ := newTestBtDropdown(t, st)
	c := d.card
	arrangeDropdown(t, c.root, 320, 400)
	// The popover's focus handle: the test wires it to the router, the
	// attach does in the app.
	router := &widget.Router{Root: c.root}
	c.focus = func(w widget.Widget) { router.SetFocus(w) }
	focused := c.pinDigits[2]
	router.SetFocus(focused)
	if router.Focused() != widget.Widget(focused) {
		t.Fatal("the middle digit box did not take focus")
	}
	// A filled box's backspace clears it in place; focus stays.
	focused.SetText("7")
	router.KeyAction(widget.KeyBackspace, 0)
	if focused.Text() != "" {
		t.Errorf("backspace on a filled box left %q", focused.Text())
	}
	if router.Focused() != widget.Widget(focused) {
		t.Error("a filled box's backspace moved focus")
	}
	// An empty box's backspace clears and focuses the previous one.
	router.SetFocus(c.pinDigits[1])
	c.pinDigits[0].SetText("9")
	router.KeyAction(widget.KeyBackspace, 0)
	if c.pinDigits[0].Text() != "" {
		t.Errorf("the previous box kept %q", c.pinDigits[0].Text())
	}
	if router.Focused() != widget.Widget(c.pinDigits[0]) {
		t.Error("the previous box did not take focus")
	}
	// Other actions reach the entry: a delete at the end of the text
	// deletes nothing forward.
	router.SetFocus(c.pinDigits[1])
	c.pinDigits[1].SetText("5")
	router.KeyAction(widget.KeyDelete, 0)
	if c.pinDigits[1].Text() != "5" {
		t.Errorf("the delete deleted backward: %q", c.pinDigits[1].Text())
	}
}
