package bar

import (
	"context"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/service/network/secrets"
)

// clickAt presses and releases through a Router on target, arranged
// under root — the real input path for a row's SetOnClickWithin and
// its buttons. It runs on the headless loop, as input does, so a
// refresher applying a re-read never races the layout or the click.
func clickAt(t *testing.T, root, target widget.Widget) {
	t.Helper()
	onHeadlessLoop(func() bool {
		root.Measure(widget.Constraints{Max: widget.Size{W: 500, H: 400}})
		root.Arrange(render.Rect{X: 0, Y: 0, W: 500, H: 400})
		b := target.(widget.Boundser).Bounds()
		p := widget.Point{X: b.X + b.W/2, Y: b.Y + b.H/2}
		router := &widget.Router{Root: root}
		router.Move(p)
		router.Press(widget.BTNLeft, p)
		router.Release(widget.BTNLeft, p)
		return true
	})
}

// fakeWifiCtl is a scripted wifiControl.
type fakeWifiCtl struct {
	mu        sync.Mutex
	aps       []network.AccessPoint
	known     map[string]bool
	scans     int
	scanGate  chan struct{}
	connects  []fakeConnect
	connectFn func(progress func(network.DeviceState)) error
	forgotten []string
	enabled   []bool
	disconn   int
}

type fakeConnect struct {
	path     dbus.ObjectPath
	password string
}

func (f *fakeWifiCtl) AccessPoints(context.Context) ([]network.AccessPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.aps), nil
}

func (f *fakeWifiCtl) KnownSSIDs() map[string]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.known)
}

func (f *fakeWifiCtl) ScanAndWait(ctx context.Context, _ time.Duration) {
	f.mu.Lock()
	f.scans++
	gate := f.scanGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
}

func (f *fakeWifiCtl) ConnectAndWait(_ context.Context, ap dbus.ObjectPath, password string, progress func(network.DeviceState)) error {
	f.mu.Lock()
	f.connects = append(f.connects, fakeConnect{ap, password})
	fn := f.connectFn
	f.mu.Unlock()
	if fn != nil {
		return fn(progress)
	}
	return nil
}

func (f *fakeWifiCtl) Disconnect(context.Context) error {
	f.mu.Lock()
	f.disconn++
	f.mu.Unlock()
	return nil
}

func (f *fakeWifiCtl) Forget(_ context.Context, ssid string) {
	f.mu.Lock()
	f.forgotten = append(f.forgotten, ssid)
	f.mu.Unlock()
}

func (f *fakeWifiCtl) SetEnabled(_ context.Context, on bool) error {
	f.mu.Lock()
	f.enabled = append(f.enabled, on)
	f.mu.Unlock()
	return nil
}

func (f *fakeWifiCtl) setAPs(aps ...network.AccessPoint) {
	f.mu.Lock()
	f.aps = aps
	f.mu.Unlock()
}

func (f *fakeWifiCtl) calls() ([]fakeConnect, []string, []bool, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.connects), slices.Clone(f.forgotten), slices.Clone(f.enabled), f.scans, f.disconn
}

// fakeAgent is a scripted secretAgent.
type fakeAgent struct {
	mu        sync.Mutex
	req       *secrets.Request
	submitted []map[string]string
	cancels   int
}

func (a *fakeAgent) Request() (secrets.Request, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.req == nil {
		return secrets.Request{}, false
	}
	return *a.req, true
}

func (a *fakeAgent) Submit(values map[string]string) {
	a.mu.Lock()
	a.submitted = append(a.submitted, values)
	a.req = nil
	a.mu.Unlock()
}

func (a *fakeAgent) Cancel() {
	a.mu.Lock()
	a.cancels++
	a.req = nil
	a.mu.Unlock()
}

func netAP(ssid string, strength uint8, sec network.SecurityType, path string) network.AccessPoint {
	return network.AccessPoint{SSID: []byte(ssid), Strength: strength, Security: sec, Path: dbus.ObjectPath(path)}
}

func newNetTestView(t *testing.T, snap network.Snapshot, deps netDeps) (*networkView, *fakeNetworkSource, *fakePopover) {
	t.Helper()
	ctx := newTestContext(t, config.Defaults())
	src := &fakeNetworkSource{snap: snap, ticks: make(chan struct{}, 2)}
	ctx.Network = src
	v := newNetworkView(ctx, deps)
	pop := &fakePopover{}
	v.attachPopover(pop)
	t.Cleanup(v.dropdownClosed)
	return v, src, pop
}

func TestNetworkDropdownWithoutWifi(t *testing.T) {
	v, _, _ := newNetTestView(t, network.Snapshot{WiredConnected: true, WiredSpeed: 1000, WiredIP4: "10.0.0.5"}, netDeps{})
	if v.headerIcon.Name() != "cm-wired-symbolic" || v.wifiSwitch.Visible() || v.scanBtn.Visible() {
		t.Error("no wifi: want the wired icon, no switch, no scan")
	}
	if !v.noAdapter.Visible() || v.listCard.Visible() || v.availLabel.Visible() {
		t.Error("no wifi: want the no-adapter state")
	}
	if !v.active.Visible() || !v.active.wired.Visible() || v.active.wiredDetail.Text() != "10.0.0.5 - 1 Gbps" {
		t.Errorf("wired card: visible %v detail %q", v.active.wired.Visible(), v.active.wiredDetail.Text())
	}
	if v.active.wifiCard.Visible() || v.active.label.Text() != i18n.T("dropdown-network-active-connection") {
		t.Error("wifi card shows without wifi, or the label is plural")
	}
}

func TestNetworkDropdownListsAndConnects(t *testing.T) {
	wifi := &fakeWifiCtl{
		aps: []network.AccessPoint{
			netAP("home", 80, network.SecurityWPA2, "/ap/1"),
			netAP("cafe", 60, network.SecurityNone, "/ap/2"),
			netAP("mine", 90, network.SecurityWPA2, "/ap/3"),
		},
		known: map[string]bool{"saved": true},
	}
	snap := network.Snapshot{WifiEnabled: true, WifiConnected: true, WifiSSID: "mine", WifiStrength: 90, WifiFrequency: 5180, WifiIP4: "192.168.1.9"}
	v, src, pop := newNetTestView(t, snap, netDeps{wifiCtl: wifi})

	if v.headerIcon.Name() != "tb-wifi-symbolic" || !v.wifiSwitch.On() || !v.scanBtn.Visible() {
		t.Error("wifi header")
	}
	if got := len(v.cache); got != 2 {
		t.Fatalf("list = %d, want home and cafe (the connected one dropped)", got)
	}
	if _, _, _, scans, _ := wifi.calls(); scans != 0 {
		t.Error("a populated list scanned on open")
	}
	// Reopening the cached instance still scans nothing while the list
	// has networks (NetworkDropdownMsg::Opened).
	v.dropdownOpened()
	if _, _, _, scans, _ := wifi.calls(); scans != 0 {
		t.Error("a populated list scanned on reopen")
	}
	if v.active.wifiName.Text() != "mine" || v.active.wifiDetail.Text() != "192.168.1.9 - 5 GHz" || !v.active.wifiStatus.Visible() {
		t.Errorf("wifi card = %q / %q", v.active.wifiName.Text(), v.active.wifiDetail.Text())
	}

	// A secured unknown network asks for its password, focused.
	v.selectNetwork("home")
	if v.state != netPasswordEntry || !v.password.Visible() || pop.focused != v.password.secret.entry {
		t.Fatalf("state %d form %v focus %v", v.state, v.password.Visible(), pop.focused)
	}
	// Step reported, then a refused password asks again, saying so.
	stepped, refuse := make(chan struct{}), make(chan struct{})
	wifi.connectFn = func(progress func(network.DeviceState)) error {
		progress(network.DeviceConfig)
		close(stepped)
		// Hold the refusal until the connecting state was checked: the
		// attempt runs off the loop and would race the assertion.
		<-refuse
		return &network.ConnectError{Failure: network.FailureAuth}
	}
	v.password.secret.entry.SetText("hunter2")
	v.password.connect()
	if onHeadlessLoop(func() bool { return v.state != netConnecting || v.active.wifiName.Text() == "" }) {
		t.Error("connecting state not shown")
	}
	close(refuse)
	waitHeadless(t, "the re-prompt", func() bool { return v.state == netPasswordEntry && v.password.errLabel.Visible() })
	<-stepped
	if v.password.errLabel.Text() != i18n.T("dropdown-network-error-wrong-password") {
		t.Errorf("error = %q", v.password.errLabel.Text())
	}
	if c, _, _, _, _ := wifi.calls(); len(c) != 1 || c[0].path != "/ap/1" || c[0].password != "hunter2" {
		t.Errorf("connect = %+v", c)
	}
	if v.password.secret.entry.Text() != "" {
		t.Error("the refused password stayed in the box")
	}

	// An open network connects straight away; a timeout shows the error
	// on the wifi card, which the dismiss clears.
	v.password.cancel()
	wifi.connectFn = func(func(network.DeviceState)) error {
		return &network.ConnectError{Failure: network.FailureTimeout}
	}
	src.setSnap(network.Snapshot{WifiEnabled: true})
	v.apply(v.read(context.Background()))
	v.selectNetwork("cafe")
	waitHeadless(t, "the failure", func() bool { return v.progress.err != "" })
	if v.active.wifiDetail.Text() != i18n.T("dropdown-network-error-timeout") || !v.active.badge.Visible() || v.active.wifiIcon.Name() != "cm-wireless-disabled-symbolic" {
		t.Errorf("error card = %q badge %v", v.active.wifiDetail.Text(), v.active.badge.Visible())
	}
	v.active.hovered = true
	v.active.syncTrailing()
	if v.active.trailing.Visible() != "error-actions" {
		t.Errorf("hovered error card shows %q", v.active.trailing.Visible())
	}
	v.active.dismiss()
	if v.progress.err != "" || v.active.wifiCard.Visible() {
		t.Error("dismiss kept the error")
	}
}

func TestNetworkDropdownStaleSearchAndForget(t *testing.T) {
	wifi := &fakeWifiCtl{aps: []network.AccessPoint{netAP("cafe", 60, network.SecurityWPA2, "/ap/2")}, known: map[string]bool{"cafe": true}}
	v, _, _ := newNetTestView(t, network.Snapshot{WifiEnabled: true}, netDeps{wifiCtl: wifi})
	// NM prunes the network: it stays listed, stale.
	wifi.setAPs()
	v.apply(v.read(context.Background()))
	if len(v.cache) != 1 || !v.cache[0].stale {
		t.Fatalf("cache = %+v, want cafe kept stale", v.cache)
	}
	// Selecting it scans; still gone, it reports not found.
	wifi.scanGate = make(chan struct{})
	v.selectNetwork("cafe")
	if v.state != netScanning || v.progress.step != i18n.T("dropdown-network-step-searching") || !v.scanBtn.HasClass("scanning") {
		t.Errorf("searching: state %d step %q", v.state, v.progress.step)
	}
	close(wifi.scanGate)
	waitHeadless(t, "the not-found", func() bool { return v.progress.err == i18n.T("dropdown-network-error-not-found") })
	if c, _, _, _, _ := wifi.calls(); len(c) != 0 {
		t.Error("a stale row connected to its dead path")
	}
	// A known network can be forgotten from its row.
	v.forget("cafe")
	waitHeadless(t, "the forget", func() bool { _, f, _, _, _ := wifi.calls(); return len(f) == 1 })
}

func TestNetworkDropdownScansWhenEmptyAndToggles(t *testing.T) {
	wifi := &fakeWifiCtl{}
	v, _, _ := newNetTestView(t, network.Snapshot{WifiEnabled: true}, netDeps{wifiCtl: wifi})
	waitHeadless(t, "the opening scan", func() bool { _, _, _, s, _ := wifi.calls(); return s == 1 && v.state == netNormal })
	if onHeadlessLoop(func() bool { return !v.noNetworks.Visible() || v.listCard.Visible() }) {
		t.Error("an empty scan: want the no-networks state")
	}
	// The view lives on the loop: the test drives it there, never
	// beside a refresh the toggle's goroutine hands back.
	onHeadlessLoop(func() bool { v.wifiSwitch.SetOn(false); return true })
	waitHeadless(t, "the toggle", func() bool { _, _, e, _, _ := wifi.calls(); return len(e) == 1 && !e[0] })
	// A backend change moves the switch without writing back.
	onHeadlessLoop(func() bool { v.apply(netState{snap: network.Snapshot{WifiEnabled: true}}); return true })
	if _, _, e, _, _ := wifi.calls(); len(e) != 1 {
		t.Error("syncing the switch wrote wifi state back")
	}
}

func TestNetworkSecretForm(t *testing.T) {
	agent := &fakeAgent{req: &secrets.Request{
		UUID: "u1", Name: "Work VPN", Setting: "vpn", Message: "Enter the code",
		Fields: []secrets.Field{{Key: "username", Label: "User"}, {Key: "password", Label: "Pass", Secret: true}, {Key: "otp", Label: "Code"}},
	}}
	v, _, pop := newNetTestView(t, network.Snapshot{}, netDeps{agent: agent})
	f := v.secret
	if !f.Visible() || f.name.Text() != "Work VPN" || !f.message.Visible() || len(f.entries) != 3 {
		t.Fatalf("form: visible %v name %q entries %d", f.Visible(), f.name.Text(), len(f.entries))
	}
	labels := f.fields.Children()
	if labels[0].(*widget.Label).Text() != i18n.T("dropdown-network-secret-username") || labels[4].(*widget.Label).Text() != "Code" {
		t.Error("field labels: want the dropdown's wording, else the service's")
	}
	if f.entries[1].entry.Echo() != widget.EchoPassword || f.entries[0].entry.Echo() != widget.EchoNormal {
		t.Error("only the secret field is masked")
	}
	if pop.focused != f.entries[0].entry {
		t.Error("the first field did not take focus")
	}
	f.entries[0].entry.SetText("me")
	f.entries[1].entry.SetText("pw")
	f.entries[2].entry.SetText("123")
	f.submit()
	if len(agent.submitted) != 1 || agent.submitted[0]["username"] != "me" || agent.submitted[0]["password"] != "pw" || agent.submitted[0]["otp"] != "123" {
		t.Errorf("submitted = %v", agent.submitted)
	}
	if f.Visible() {
		t.Error("the form stayed up after submit")
	}
	// A new prompt, cancelled.
	agent.mu.Lock()
	agent.req = &secrets.Request{UUID: "u2", Name: "Other", Fields: []secrets.Field{{Key: "psk", Secret: true}}}
	agent.mu.Unlock()
	v.apply(v.read(context.Background()))
	f.cancel()
	if agent.cancels != 1 || f.Visible() {
		t.Error("cancel did not reach the agent")
	}
}

// The wifi rows are the plain boxes Rust's GestureClick lands on
// (network_item.rs:49-190): not buttons, pointer cursor, and the click
// hits anywhere in the row — the trailing lock icon included. A known
// row's hover swaps the lock for the actions, and then the forget
// button — a button — keeps its own click and does not select the row.
func TestNetworkRowIsAClickableBoxNotAButton(t *testing.T) {
	wifi := &fakeWifiCtl{
		aps: []network.AccessPoint{
			netAP("cafe", 60, network.SecurityWPA2, "/ap/2"),
			netAP("saved", 70, network.SecurityWPA2, "/ap/3"),
		},
		known: map[string]bool{"saved": true},
	}
	v, _, pop := newNetTestView(t, network.Snapshot{WifiEnabled: true}, netDeps{wifiCtl: wifi})
	if len(v.list.Children()) != 2 {
		t.Fatalf("rows = %d, want cafe and saved", len(v.list.Children()))
	}
	// Strongest first: the saved network outranks the unknown one.
	saved, ok := v.list.Children()[0].(*widget.Box)
	if !ok {
		t.Fatalf("row = %T, want a plain box", v.list.Children()[0])
	}
	cafe, ok := v.list.Children()[1].(*widget.Box)
	if !ok {
		t.Fatalf("row = %T, want a plain box", v.list.Children()[1])
	}
	if _, isButton := any(cafe).(*widget.Button); isButton {
		t.Fatal("the row is a button element")
	}
	if !cafe.HasClass("network-item") || widget.HasClass(cafe, "network-item-pick") {
		t.Error("the row is not the network-item box")
	}
	if widget.CursorNameOf(cafe) != "pointer" {
		t.Error("the row does not carry the pointer cursor")
	}

	// The trailing lock icon is a plain widget: clicking it selects; a
	// secured unknown network asks for its password, focused.
	var lock widget.Widget
	walkTree(cafe, func(w widget.Widget) bool {
		if i, ok := w.(*widget.Icon); ok && i.HasClass("network-item-lock") {
			lock = i
		}
		return lock == nil
	})
	if lock == nil {
		t.Fatal("no lock icon in the row")
	}
	clickAt(t, cafe, lock)
	if v.state != netPasswordEntry || pop.focused != v.password.secret.entry {
		t.Errorf("a click on the trailing icon did not select: state %d focus %v", v.state, pop.focused)
	}

	// The forget button on the saved row forgets; it does not select the
	// row. The hover that reveals it is the pointer clickAt moves.
	var trailing *widget.Stack
	walkTree(saved, func(w widget.Widget) bool {
		if s, ok := w.(*widget.Stack); ok && s.HasClass("network-item-trailing") {
			trailing = s
		}
		return trailing == nil
	})
	if trailing == nil {
		t.Fatal("no trailing stack on the saved row")
	}
	trailing.Show("actions")
	forget := findButton(trailing, "network-item-forget")
	if forget == nil {
		t.Fatal("no forget button in the row")
	}
	if !widget.HasClass(forget, "ghost") || widget.HasClass(forget, "ghost-icon") {
		t.Error("the forget button is not GhostButton + network-item-forget")
	}
	clickAt(t, saved, forget)
	waitHeadless(t, "the forget", func() bool { _, f, _, _, _ := wifi.calls(); return len(f) == 1 && f[0] == "saved" })
	if v.selection != nil && v.selection.ssid == "saved" {
		t.Error("the forget button also selected the row")
	}

	// The saved row's name still selects the row itself.
	var name widget.Widget
	walkTree(saved, func(w widget.Widget) bool {
		if l, ok := w.(*widget.Label); ok && l.HasClass("network-item-name") {
			name = l
		}
		return name == nil
	})
	if name == nil {
		t.Fatal("no name label in the row")
	}
	clickAt(t, saved, name)
	// The selection went straight to connecting: the watch records the
	// connect, and its completion lands back on the loop.
	waitHeadless(t, "the connect", func() bool {
		c, _, _, _, _ := wifi.calls()
		return len(c) == 1 && c[0].path == "/ap/3" && c[0].password == ""
	})
	waitHeadless(t, "the connect finished", func() bool { return v.state == netNormal })
}

// .dropdown-content is on the root content wrapper — active
// connections, secret form, and body stack under one padded box — and
// nowhere else in the tree (network/mod.rs:129-183).
func TestNetworkDropdownContentOnTheRootWrapper(t *testing.T) {
	v, _, _ := newNetTestView(t, network.Snapshot{}, netDeps{})
	if widget.HasClass(v.Box, "dropdown-content") {
		t.Error("the frame wears .dropdown-content")
	}
	var content *widget.Box
	found := 0
	walkTree(v, func(w widget.Widget) bool {
		if b, ok := w.(*widget.Box); ok && widget.HasClass(b, "dropdown-content") {
			found++
			for _, kid := range v.Children() {
				if kid == widget.Widget(b) {
					content = b
				}
			}
		}
		return true
	})
	if found != 1 || content == nil {
		t.Fatalf(".dropdown-content boxes = %d, want the one root wrapper", found)
	}
	kids := content.Children()
	if len(kids) != 3 {
		t.Fatalf("content children = %d, want active, secret, and the body stack", len(kids))
	}
	if kids[0] != widget.Widget(v.active) || kids[1] != widget.Widget(v.secret) {
		t.Error("the active connections and secret form are not under the content wrapper")
	}
	if kids[2] != widget.Widget(v.body) {
		t.Error("the body stack is not under the content wrapper")
	}
}

// The scan button is GhostIconButton: ghost-icon, never ghost
// (network/mod.rs:97-109).
func TestNetworkScanButtonGhostIconOnly(t *testing.T) {
	v, _, _ := newNetTestView(t, network.Snapshot{WifiEnabled: true}, netDeps{wifiCtl: &fakeWifiCtl{}})
	// The empty list scans on open; its completion restyles the button
	// off the test goroutine, so read on the loop.
	ghostIcon, ghost := onHeadlessLoop(func() bool {
		return widget.HasClass(v.scanBtn, "ghost-icon") && widget.HasClass(v.scanBtn, "network-scan-btn")
	}), onHeadlessLoop(func() bool { return widget.HasClass(v.scanBtn, "ghost") })
	if !ghostIcon {
		t.Error("the scan button is not ghost-icon + network-scan-btn")
	}
	if ghost {
		t.Error("the scan button carries the ghost class")
	}
}

// The empty states' icons carry the sm size class (available_networks/
// mod.rs:114,137).
func TestNetworkEmptyStatesCarryTheSmIcon(t *testing.T) {
	v, _, _ := newNetTestView(t, network.Snapshot{}, netDeps{})
	for _, empty := range []*widget.Box{v.noAdapter, v.noNetworks} {
		var icon *widget.Icon
		walkTree(empty, func(w widget.Widget) bool {
			if i, ok := w.(*widget.Icon); ok && i.HasClass("icon") {
				icon = i
			}
			return icon == nil
		})
		if icon == nil {
			t.Fatal("the empty state has no icon")
		}
		if !icon.HasClass("sm") {
			t.Error("the empty state icon lacks the sm class")
		}
	}
}

// The wifi tile swaps wifi for error; it never wears both
// (active_connections/methods.rs:100-110).
func TestNetworkWifiTileSwapsWifiForError(t *testing.T) {
	v, _, _ := newNetTestView(t, network.Snapshot{WifiEnabled: true}, netDeps{wifiCtl: &fakeWifiCtl{}})
	onHeadlessLoop(func() bool {
		v.progress = netProgress{err: "no answer"}
		v.active.apply(v.cur.snap, v.progress)
		return true
	})
	if !v.active.wifiTile.HasClass("error") || v.active.wifiTile.HasClass("wifi") {
		t.Error("a failed join left the tile in its wifi state")
	}
	onHeadlessLoop(func() bool {
		v.progress = netProgress{}
		v.active.apply(v.cur.snap, v.progress)
		return true
	})
	if !v.active.wifiTile.HasClass("wifi") || v.active.wifiTile.HasClass("error") {
		t.Error("the tile did not return to its wifi state")
	}
}
