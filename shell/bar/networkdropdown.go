package bar

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/service/network/secrets"
)

// netScanTimeout is SCAN_TIMEOUT.
const netScanTimeout = 30 * time.Second

// netListState is ListState.
type netListState int

const (
	netNormal netListState = iota
	netPasswordEntry
	netConnecting
	netScanning
)

// netState is one read of NetworkManager for the dropdown.
type netState struct {
	snap    network.Snapshot
	aps     []network.AccessPoint
	known   map[string]bool
	request secrets.Request
	asking  bool
	vpns    []network.VPN
}

// netProgress is ConnectionProgress: the network being joined, its
// current step, and the failure that ended it.
type netProgress struct {
	ssid, step, err string
}

// networkView is the network dropdown: the header (wifi or wired icon,
// scan, the wifi switch), the active connections, NetworkManager's
// pending credential prompt, the VPNs, and the available networks with
// their password form.
type networkView struct {
	ctx  ModuleContext
	font render.Font
	px   float64
	popoverHook

	*widget.Box
	headerIcon  *widget.Icon
	scanBtn     *widget.Button
	wifiSwitch  *widget.Switch
	syncing     bool
	active      *netActive
	secret      *netSecretForm
	vpns        *netVPNs
	vpnForm     *netVPNForm
	body        *widget.Stack
	availLabel  *widget.Label
	password    *netPasswordForm
	listCard    *widget.Box
	list        *widget.Box
	noNetworks  *widget.Box
	noAdapter   *widget.Box
	state       netListState
	cur         netState
	cache       []apSnapshot
	selection   *apSnapshot
	pendingSSID string
	progress    netProgress

	netDeps

	// reads is the view's one reader: NetworkManager ticks, the other
	// feeds, and every re-read after a write go through it.
	reads *refresher[netState]

	once   sync.Once
	life   context.Context
	cancel context.CancelFunc
}

// wifiControl is the wireless device the dropdown drives
// (*network.Wifi).
type wifiControl interface {
	AccessPoints(ctx context.Context) ([]network.AccessPoint, error)
	KnownSSIDs() map[string]bool
	ScanAndWait(ctx context.Context, timeout time.Duration)
	ConnectAndWait(ctx context.Context, ap dbus.ObjectPath, password string, progress func(network.DeviceState)) error
	Disconnect(ctx context.Context) error
	Forget(ctx context.Context, ssid string)
	SetEnabled(ctx context.Context, on bool) error
}

// secretAgent is NetworkManager's pending credential prompt
// (*network.Agent).
type secretAgent interface {
	Request() (secrets.Request, bool)
	Submit(values map[string]string)
	Cancel()
}

// netDeps are the services the dropdown drives beside the snapshot
// source; nil members are absent (no wifi device, no agent).
type netDeps struct {
	wifiCtl wifiControl
	agent   secretAgent
	vpn     vpnControl
	// vpnKinds lists the kinds the VPN editor offers
	// (network.AvailableKinds).
	vpnKinds func() []network.VPNKind
	// openFile is the file chooser a wg-quick import goes through.
	openFile fileOpener
	// changes are the extra feeds a re-read follows (saved profiles,
	// the agent, the VPNs).
	changes []func(context.Context) <-chan struct{}
}

// networkDeps wires the dropdown to the live network service.
func networkDeps(n *network.Service) netDeps {
	d := netDeps{vpnKinds: network.AvailableKinds, openFile: openFileOnSessionBus}
	if n == nil {
		return d
	}
	if n.Wifi != nil {
		d.wifiCtl = n.Wifi
	}
	if n.Agent != nil {
		d.agent = n.Agent
		d.changes = append(d.changes, n.Agent.Changes)
	}
	if n.Settings != nil {
		d.changes = append(d.changes, n.Settings.Changes)
	}
	if n.VPN != nil {
		d.vpn = n.VPN
		d.changes = append(d.changes, n.VPN.Changes)
	}
	return d
}

func networkDropdown(ctx ModuleContext) widget.Widget {
	return newNetworkView(ctx, networkDeps(ctx.Networking))
}

func newNetworkView(ctx ModuleContext, deps netDeps) *networkView {
	font, px := dropdownFont(ctx)
	v := &networkView{ctx: ctx, font: font, px: px, netDeps: deps}
	life, cancel := context.WithCancel(context.Background()) //nolint:gosec // dropdownClosed cancels it
	v.life, v.cancel = life, cancel
	v.reads = newRefresher(ctx, life, v.read, v.apply)
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "network-dropdown")

	scanIcon := widget.NewThemeIcon("tb-refresh-symbolic", int(px))
	v.scanBtn = dropdownButton(scanIcon, "network-scan-btn", v.startScan)
	v.scanBtn.AddClass("ghost-icon")
	v.wifiSwitch = widget.NewSwitch(false)
	v.wifiSwitch.OnChanged = v.wifiToggled
	header, icon, _ := dropdownHeaderParts(font, px, "tb-wifi-symbolic", i18n.T("dropdown-network-title"), v.scanBtn, v.wifiSwitch)
	v.headerIcon = icon
	v.Append(header, false)

	v.active = newNetActive(v)
	v.Append(v.active, false)
	v.secret = newNetSecretForm(v)
	v.Append(v.secret, false)

	browse := widget.NewBox(widget.Column, 10, 0)
	browse.AddClass("dropdown-content", "network-content")
	v.vpns = newNetVPNs(v)
	browse.Append(v.vpns, false)
	v.availLabel = v.sectionLabel(i18n.T("dropdown-network-available"))
	browse.Append(v.availLabel, false)
	v.password = newNetPasswordForm(v)
	browse.Append(v.password, false)
	v.list = widget.NewBox(widget.Column, 2, 0)
	v.listCard = widget.NewBox(widget.Column, 0, 6)
	v.listCard.AddClass("card", "network-list")
	v.listCard.Append(dropdownScroll(v.list, ""), true)
	browse.Append(v.listCard, true)
	v.noNetworks = emptyState(font, px, "cm-wireless-disabled-symbolic", i18n.T("dropdown-network-no-networks-title"), i18n.T("dropdown-network-no-networks-description"))
	v.noAdapter = emptyState(font, px, "tb-wifi-off-symbolic", i18n.T("dropdown-network-no-adapter-title"), i18n.T("dropdown-network-no-adapter-description"))
	browse.Append(v.noNetworks, false)
	browse.Append(v.noAdapter, false)
	// Two pages rather than a form hiding the lists in place: the editor
	// is somewhere you go, and coming back is one button. Each page is its
	// own height (vhomogeneous off) and the card tweens between them; the
	// panel reserves the taller page up front (dropdown_resize).
	v.vpnForm = newNetVPNForm(v)
	v.body = widget.NewStack()
	v.body.SetTransition(widget.StackSlideLeftRight, gtkStackDuration)
	v.body.SetHomogeneous(false)
	v.body.SetInterpolateSize(true)
	v.body.Add(netPageBrowse, browse)
	v.body.Add(netPageEdit, v.vpnForm)
	v.Append(v.body, true)

	v.apply(v.read(context.Background()))
	v.scanIfEmpty()
	v.follow()
	return v
}

func (v *networkView) sectionLabel(text string) *widget.Label {
	l := widget.NewLabel(v.font, v.px*0.85, text, 0)
	l.AddClass("section-label")
	return l
}

// wifi is the wireless device, nil without one.
func (v *networkView) wifi() wifiControl { return v.wifiCtl }

// read snapshots NetworkManager off the loop.
func (v *networkView) read(ctx context.Context) netState {
	var s netState
	if v.ctx.Network != nil {
		if snap, err := v.ctx.Network.Read(ctx); err == nil {
			s.snap = snap
		}
	}
	if w := v.wifi(); w != nil {
		s.aps, _ = w.AccessPoints(ctx)
		s.known = w.KnownSSIDs()
	}
	if v.agent != nil {
		s.request, s.asking = v.agent.Request()
	}
	if v.vpn != nil {
		s.vpns = v.vpn.Entries()
	}
	return s
}

// apply pushes one read into every part.
func (v *networkView) apply(s netState) {
	v.cur = s
	wifiAvail := v.wifi() != nil
	if wifiAvail {
		v.headerIcon.SetThemeName("tb-wifi-symbolic")
	} else {
		v.headerIcon.SetThemeName("cm-wired-symbolic")
	}
	v.syncing = true
	v.wifiSwitch.SetOn(s.snap.WifiEnabled)
	v.syncing = false
	v.wifiSwitch.SetVisible(wifiAvail)
	v.scanBtn.SetVisible(wifiAvail && s.snap.WifiEnabled)
	v.scanBtn.SetEnabled(v.state != netScanning)
	if v.state == netScanning {
		v.scanBtn.AddClass("scanning")
	} else {
		v.scanBtn.RemoveClass("scanning")
	}
	if s.snap.WifiConnected {
		v.progress = netProgress{}
	}
	v.active.apply(s.snap, v.progress)
	v.secret.apply(s.request, s.asking)
	v.vpns.apply(s.vpns)
	if !s.snap.WifiEnabled && wifiAvail {
		v.cache = nil
	}
	v.rebuildList()
}

// rebuildList is rebuild_network_list.
func (v *networkView) rebuildList() {
	wifiAvail := v.wifi() != nil
	connected := ""
	if v.cur.snap.WifiConnected {
		connected = v.cur.snap.WifiSSID
	}
	live := sortedUniqueAccessPoints(v.cur.aps, connected, v.cur.known)
	// NM can drop the whole list for a moment mid-scan; folding that in
	// would flash every row stale until the results land.
	if len(live) > 0 || len(v.cache) == 0 || v.state != netScanning {
		v.cache = mergeWithCache(live, v.cache, connected, v.cur.known)
	}
	v.list.Clear()
	for _, ap := range v.cache {
		v.list.Append(v.row(ap), false)
	}
	v.availLabel.SetVisible(wifiAvail)
	v.password.SetVisible(wifiAvail && v.state == netPasswordEntry)
	v.listCard.SetVisible(wifiAvail && len(v.cache) > 0)
	v.noNetworks.SetVisible(wifiAvail && len(v.cache) == 0)
	v.noAdapter.SetVisible(!wifiAvail)
}

// row is NetworkItem: the signal icon, the SSID over its security
// line, and the lock, or on hover the forget action for a saved one.
func (v *networkView) row(ap apSnapshot) widget.Widget {
	content := widget.NewBox(widget.Row, 10, 0)
	signal := widget.NewThemeIcon(signalStrengthIcon(ap.strength), int(v.px*1.1))
	signal.AddClass("network-item-signal")
	content.Append(signal, false)
	info := widget.NewBox(widget.Column, 2, 0)
	name := widget.NewLabel(v.font, v.px, ap.ssid, 0)
	name.AddClass("network-item-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	info.Append(name, false)
	sec := widget.NewLabel(v.font, v.px*0.8, rowSecurityLabel(ap), 0)
	sec.AddClass("network-item-security")
	info.Append(sec, false)
	content.Append(info, true)
	ssid := ap.ssid
	pick := dropdownButton(content, "network-item-pick", func() { v.selectNetwork(ssid) })

	row := widget.NewBox(widget.Row, 4, 0)
	row.AddClass("network-item")
	if ap.stale {
		row.AddClass("stale")
	}
	row.Append(pick, true)
	secured := ap.security.RequiresPassword()
	if secured || ap.known {
		trailing := widget.NewStack()
		trailing.AddClass("network-item-trailing")
		trailing.SetTransition(widget.StackCrossfade, hoverTransition)
		lock := widget.NewBox(widget.Row, 0, 0)
		if secured {
			icon := widget.NewThemeIcon("ld-lock-symbolic", int(v.px*0.9))
			icon.AddClass("network-item-lock")
			lock.Append(icon, false)
		}
		trailing.Add("lock", lock)
		if ap.known {
			actions := widget.NewBox(widget.Row, 0, 0)
			actions.AddClass("network-item-actions")
			forget := widget.NewLabel(v.font, v.px*0.85, i18n.T("dropdown-network-forget"), 0)
			actions.Append(dropdownButton(forget, "network-item-forget", func() { v.forget(ssid) }), false)
			trailing.Add("actions", actions)
			row.SetOnHoverWithin(func(on bool) {
				if on {
					trailing.Show("actions")
				} else {
					trailing.Show("lock")
				}
			})
		}
		trailing.Show("lock")
		row.Append(trailing, false)
	}
	return row
}

// find is the cached entry for ssid.
func (v *networkView) find(ssid string) (apSnapshot, bool) {
	for _, ap := range v.cache {
		if ap.ssid == ssid {
			return ap, true
		}
	}
	return apSnapshot{}, false
}

// selectNetwork is select_network: a stale row scans for its network
// first; a secured unknown one asks for its password; anything else
// connects.
func (v *networkView) selectNetwork(ssid string) {
	ap, ok := v.find(ssid)
	if !ok {
		return
	}
	v.pendingSSID = ""
	if ap.stale {
		v.pendingSSID = ssid
		v.progress = netProgress{ssid: ssid, step: i18n.T("dropdown-network-step-searching")}
		v.active.apply(v.cur.snap, v.progress)
		if v.state != netScanning {
			v.startScan()
		}
		return
	}
	v.selection = &ap
	if ap.security.RequiresPassword() && !ap.known {
		v.state = netPasswordEntry
		v.password.show(ap, "")
		v.rebuildList()
		return
	}
	v.connect("")
}

// connect is connect_to_selected: the watch runs off the loop and
// reports each step and the outcome back.
func (v *networkView) connect(password string) {
	w, sel := v.wifi(), v.selection
	if w == nil || sel == nil {
		return
	}
	v.state = netConnecting
	v.progress = netProgress{ssid: sel.ssid}
	v.active.apply(v.cur.snap, v.progress)
	v.rebuildList()
	path := sel.path
	go func() {
		err := w.ConnectAndWait(context.Background(), path, password, func(state network.DeviceState) {
			if step := connectionStep(state); step != "" {
				v.ctx.Invoke(func() {
					v.progress.step = step
					v.active.apply(v.cur.snap, v.progress)
				})
			}
		})
		v.ctx.Invoke(func() { v.connectDone(err) })
	}()
}

// connectDone is the connection watcher's verdict.
func (v *networkView) connectDone(err error) {
	var ce *network.ConnectError
	switch {
	case err == nil:
		v.state, v.selection = netNormal, nil
		v.progress = netProgress{}
	case errors.As(err, &ce) && ce.Failure == network.FailureAuth && v.selection != nil:
		// Wrong password: ask again, saying so.
		v.state = netPasswordEntry
		v.progress = netProgress{}
		v.password.show(*v.selection, i18n.T("dropdown-network-error-wrong-password"))
	case errors.As(err, &ce) && ce.Failure == network.FailureSuperseded:
		v.state, v.selection = netNormal, nil
		v.progress = netProgress{}
	case errors.As(err, &ce):
		v.fail(connectFailureMessage(ce.Failure))
	default:
		log.Printf("network: connect: %v", err)
		v.fail(err.Error())
	}
	v.active.apply(v.cur.snap, v.progress)
	v.rebuildList()
	v.refresh()
}

// fail is handle_connection_failure.
func (v *networkView) fail(message string) {
	v.state, v.selection = netNormal, nil
	v.progress = netProgress{err: message}
}

// scanIfEmpty is scan_if_empty: an empty list is never the first thing
// seen.
func (v *networkView) scanIfEmpty() {
	if v.wifi() != nil && v.cur.snap.WifiEnabled && len(v.cache) == 0 && v.state == netNormal {
		v.startScan()
	}
}

// startScan is start_scan: the scan runs off the loop until NM bumps
// LastScan or SCAN_TIMEOUT passes.
func (v *networkView) startScan() {
	w := v.wifi()
	if w == nil || v.state == netScanning {
		return
	}
	v.state = netScanning
	v.apply(v.cur)
	go func() {
		w.ScanAndWait(v.life, netScanTimeout)
		v.reads.requestThen(v.scanDone)
	}()
}

// scanDone is ScanComplete, after the read that followed the scan was
// applied: a pending stale click connects if its network came back,
// else reports it not found.
func (v *networkView) scanDone() {
	if v.state == netScanning {
		v.state = netNormal
	}
	v.apply(v.cur)
	if v.pendingSSID == "" {
		return
	}
	ssid := v.pendingSSID
	v.pendingSSID = ""
	if ap, ok := v.find(ssid); ok && !ap.stale {
		v.progress = netProgress{}
		v.selectNetwork(ssid)
		return
	}
	v.fail(i18n.T("dropdown-network-error-not-found"))
	v.active.apply(v.cur.snap, v.progress)
}

// forget is forget_network.
func (v *networkView) forget(ssid string) {
	w := v.wifi()
	if w == nil {
		return
	}
	go func() {
		w.Forget(context.Background(), ssid)
		v.refresh()
	}()
}

// refresh re-reads off the loop and applies, through the view's reader.
func (v *networkView) refresh() { v.reads.request() }

// wifiToggled is toggle_wifi.
func (v *networkView) wifiToggled(on bool) {
	w := v.wifi()
	if v.syncing || w == nil {
		return
	}
	if !on {
		v.cache, v.pendingSSID, v.selection = nil, "", nil
		v.state = netNormal
	}
	go func() {
		if err := w.SetEnabled(context.Background(), on); err != nil {
			log.Printf("network: wifi toggle: %v", err)
		}
		v.refresh()
	}()
}

// follow re-reads on NetworkManager, saved-profile, and agent changes
// until the dropdown closes.
func (v *networkView) follow() {
	if v.ctx.Network != nil {
		followInto(v.life, "network", v.ctx.Network.Subscribe, v.reads)
	}
	for _, feed := range v.changes {
		ch := feed(v.life)
		go func() {
			for range ch {
				v.reads.request()
			}
		}()
	}
}

// dropdownClosed implements dropdownCloser.
func (v *networkView) dropdownClosed() { v.once.Do(v.cancel) }

// netActive is ActiveConnections: the wired card and the wifi card,
// whose hover swaps its status for the disconnect/forget (or dismiss)
// actions.
type netActive struct {
	*widget.Box
	v                    *networkView
	label                *widget.Label
	wired                *widget.Box
	wiredDetail          *widget.Label
	wifiCard             *widget.Box
	wifiIcon             *widget.Icon
	wifiTile             *widget.Box
	wifiName, wifiDetail *widget.Label
	wifiStatus           *widget.Label
	badge                *widget.Label
	trailing             *widget.Stack
	hovered              bool
	connected, wifiErr   bool
	ssid                 string
}

func newNetActive(v *networkView) *netActive {
	a := &netActive{v: v, Box: widget.NewBox(widget.Column, 6, 0)}
	a.label = v.sectionLabel("")
	a.Append(a.label, false)
	group := widget.NewBox(widget.Column, 6, 8)
	group.AddClass("card", "network-connections-group")

	a.wired = widget.NewBox(widget.Row, 10, 0)
	a.wired.AddClass("network-connection-card")
	wiredIcon := widget.NewThemeIcon("cm-wired-symbolic", int(v.px*1.3))
	a.wired.Append(iconTile(wiredIcon, "network-connection-icon ethernet", ""), false)
	wiredInfo := widget.NewBox(widget.Column, 2, 0)
	wiredName := widget.NewLabel(v.font, v.px, i18n.T("dropdown-network-ethernet"), 0)
	wiredName.AddClass("network-connection-name")
	wiredInfo.Append(wiredName, false)
	a.wiredDetail = widget.NewLabel(v.font, v.px*0.8, "", 0)
	a.wiredDetail.AddClass("network-connection-detail")
	wiredInfo.Append(a.wiredDetail, false)
	a.wired.Append(wiredInfo, true)
	a.wired.Append(v.statusLabel(), false)
	group.Append(a.wired, false)

	a.wifiCard = widget.NewBox(widget.Row, 10, 0)
	a.wifiCard.AddClass("network-connection-card")
	a.wifiIcon = widget.NewThemeIcon(signalStrengthIcon(0), int(v.px*1.3))
	a.wifiTile = iconTile(a.wifiIcon, "network-connection-icon wifi", "")
	a.wifiCard.Append(a.wifiTile, false)
	info := widget.NewBox(widget.Column, 2, 0)
	a.wifiName = widget.NewLabel(v.font, v.px, "", 0)
	a.wifiName.AddClass("network-connection-name")
	a.wifiName.SetEllipsize(widget.EllipsizeEnd)
	info.Append(a.wifiName, false)
	a.wifiDetail = widget.NewLabel(v.font, v.px*0.8, "", 0)
	a.wifiDetail.AddClass("network-connection-detail")
	a.wifiDetail.SetEllipsize(widget.EllipsizeEnd)
	info.Append(a.wifiDetail, false)
	a.wifiCard.Append(info, true)

	a.trailing = widget.NewStack()
	a.trailing.AddClass("network-hover-stack")
	a.trailing.SetTransition(widget.StackCrossfade, hoverTransition)
	status := widget.NewBox(widget.Row, 0, 0)
	a.wifiStatus = v.statusLabel()
	status.Append(a.wifiStatus, false)
	a.badge = widget.NewLabel(v.font, v.px*0.8, "", 0)
	a.badge.AddClass("badge-subtle", "network-connection-status")
	status.Append(a.badge, false)
	a.trailing.Add("status", status)
	actions := widget.NewBox(widget.Row, 4, 0)
	actions.AddClass("network-connection-actions")
	actions.Append(v.ghostText(i18n.T("dropdown-network-disconnect"), "network-action-disconnect", a.disconnect), false)
	actions.Append(v.ghostText(i18n.T("dropdown-network-forget"), "network-action-forget", a.forget), false)
	a.trailing.Add("actions", actions)
	errActions := widget.NewBox(widget.Row, 0, 0)
	errActions.AddClass("network-connection-actions")
	errActions.Append(v.ghostText(i18n.T("dropdown-network-dismiss"), "network-action-dismiss", a.dismiss), false)
	a.trailing.Add("error-actions", errActions)
	a.wifiCard.Append(a.trailing, false)
	a.wifiCard.SetOnHoverWithin(func(on bool) {
		a.hovered = on
		a.syncTrailing()
	})
	group.Append(a.wifiCard, false)
	a.Append(group, false)
	return a
}

func (v *networkView) statusLabel() *widget.Label {
	l := widget.NewLabel(v.font, v.px*0.85, i18n.T("dropdown-network-connected"), 0)
	l.AddClass("network-connection-status")
	return l
}

func (v *networkView) ghostText(text, class string, onClick func()) *widget.Button {
	b := dropdownButton(widget.NewLabel(v.font, v.px*0.85, text, v.ctx.Style.fg), class, onClick)
	b.AddClass("ghost")
	return b
}

// primaryText is a PrimaryButton: the accent-filled action of a form.
func (v *networkView) primaryText(text, class string, onClick func()) *widget.Button {
	b := v.ghostText(text, class, onClick)
	b.RemoveClass("ghost")
	b.AddClass("primary")
	return b
}

// errorLabel is a form's error line: wrapped, in the error color,
// hidden until there is something to say.
func (v *networkView) errorLabel() *widget.Label {
	l := widget.NewLabel(v.font, v.px*0.8, "", 0)
	l.AddClass("network-password-error")
	l.SetWrap(true)
	l.SetVisible(false)
	return l
}

// apply is the cards' #[watch]es over the snapshot and the progress.
func (a *netActive) apply(s network.Snapshot, p netProgress) {
	connecting := p.ssid != "" || s.WifiConnecting
	a.connected = s.WifiConnected
	a.wifiErr = p.err != "" && !s.WifiConnected
	a.ssid = s.WifiSSID
	a.SetVisible(s.WifiConnected || s.WiredConnected || connecting || p.err != "")
	if s.WiredConnected && s.WifiConnected {
		a.label.SetText(i18n.T("dropdown-network-active-connections"))
	} else {
		a.label.SetText(i18n.T("dropdown-network-active-connection"))
	}

	a.wired.SetVisible(s.WiredConnected)
	speed := formatWiredSpeed(s.WiredSpeed)
	if s.WiredIP4 != "" {
		speed = s.WiredIP4 + " - " + speed
	}
	a.wiredDetail.SetText(speed)
	a.wiredDetail.SetVisible(s.WiredSpeed > 0)

	a.wifiCard.SetVisible(s.WifiConnected || connecting || p.err != "")
	if a.wifiErr {
		a.wifiIcon.SetThemeName("cm-wireless-disabled-symbolic")
		a.wifiTile.AddClass("error")
	} else {
		a.wifiIcon.SetThemeName(signalStrengthIcon(s.WifiStrength))
		a.wifiTile.RemoveClass("error")
	}
	switch {
	case s.WifiSSID != "":
		a.wifiName.SetText(s.WifiSSID)
	case p.ssid != "":
		a.wifiName.SetText(p.ssid)
	default:
		a.wifiName.SetText(i18n.T("dropdown-network-wifi"))
	}
	detail := ""
	switch {
	case p.err != "":
		detail = p.err
	case p.step != "":
		detail = p.step
	default:
		band := frequencyBand(s.WifiFrequency)
		switch {
		case s.WifiIP4 != "" && band != "":
			detail = s.WifiIP4 + " - " + band
		case s.WifiIP4 != "":
			detail = s.WifiIP4
		default:
			detail = band
		}
	}
	a.wifiDetail.SetText(detail)
	a.wifiDetail.SetVisible(detail != "")
	a.wifiDetail.SetTooltip(p.err)
	if a.wifiErr {
		a.wifiDetail.AddClass("error")
	} else {
		a.wifiDetail.RemoveClass("error")
	}

	a.wifiStatus.SetVisible(s.WifiConnected && !connecting && p.err == "")
	a.badge.RemoveClass("error", "warning")
	switch {
	case p.err != "":
		a.badge.SetText(i18n.T("dropdown-network-error"))
		a.badge.AddClass("error")
	case connecting:
		a.badge.SetText(i18n.T("dropdown-network-connecting"))
		a.badge.AddClass("warning")
	}
	a.badge.SetVisible(connecting || p.err != "")
	a.syncTrailing()
}

// syncTrailing picks the hover stack's page.
func (a *netActive) syncTrailing() {
	switch {
	case a.hovered && a.connected:
		a.trailing.Show("actions")
	case a.hovered && a.wifiErr:
		a.trailing.Show("error-actions")
	default:
		a.trailing.Show("status")
	}
}

func (a *netActive) disconnect() {
	w := a.v.wifi()
	if w == nil {
		return
	}
	go func() {
		if err := w.Disconnect(context.Background()); err != nil {
			log.Printf("network: wifi disconnect: %v", err)
		}
	}()
}

// forget is forget_wifi: the saved profiles go, then the link.
func (a *netActive) forget() {
	w, ssid := a.v.wifi(), a.ssid
	if w == nil || ssid == "" {
		return
	}
	go func() {
		w.Forget(context.Background(), ssid)
		if err := w.Disconnect(context.Background()); err != nil {
			log.Printf("network: wifi disconnect after forget: %v", err)
		}
	}()
}

// dismiss is DismissError.
func (a *netActive) dismiss() {
	a.v.progress.err = ""
	a.apply(a.v.cur.snap, a.v.progress)
}

// netPasswordForm is PasswordForm: the network's header, the masked
// password box, the error from a refused attempt, Cancel and Connect.
type netPasswordForm struct {
	*widget.Box
	v        *networkView
	signal   *widget.Icon
	ssid     *widget.Label
	security *widget.Label
	secret   *secretEntry
	errLabel *widget.Label
}

func newNetPasswordForm(v *networkView) *netPasswordForm {
	f := &netPasswordForm{v: v, Box: widget.NewBox(widget.Column, 8, 10)}
	f.AddClass("card", "network-password-card")
	header := widget.NewBox(widget.Row, 10, 0)
	header.AddClass("network-password-header")
	f.signal = widget.NewThemeIcon(signalStrengthIcon(0), int(v.px*1.3))
	header.Append(iconTile(f.signal, "network-connection-icon wifi", ""), false)
	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("network-password-info")
	f.ssid = widget.NewLabel(v.font, v.px, "", 0)
	f.ssid.AddClass("network-password-name")
	f.ssid.SetEllipsize(widget.EllipsizeEnd)
	info.Append(f.ssid, false)
	f.security = widget.NewLabel(v.font, v.px*0.8, "", 0)
	f.security.AddClass("network-password-security")
	info.Append(f.security, false)
	header.Append(info, true)
	closeIcon := widget.NewThemeIcon("ld-x-symbolic", int(v.px))
	header.Append(dropdownButton(closeIcon, "network-password-close", f.cancel), false)
	f.Append(header, false)
	f.secret = newSecretEntry(v.ctx, v.font, v.px)
	f.secret.entry.SetPlaceholder(i18n.T("dropdown-network-password-placeholder"))
	f.secret.entry.OnActivate = func(string) { f.connect() }
	f.Append(f.secret, false)
	f.errLabel = v.errorLabel()
	f.Append(f.errLabel, false)
	actions := widget.NewBox(widget.Row, 6, 0)
	actions.AddClass("network-password-actions")
	actions.Append(widget.NewSpacer(0, 0), true)
	actions.Append(v.ghostText(i18n.T("dropdown-network-cancel"), "network-password-cancel", f.cancel), false)
	actions.Append(v.primaryText(i18n.T("dropdown-network-connect"), "network-password-connect", f.connect), false)
	f.Append(actions, false)
	f.SetVisible(false)
	return f
}

// show is PasswordFormInput::Show: the box takes the keyboard at once.
func (f *netPasswordForm) show(ap apSnapshot, errMessage string) {
	f.signal.SetThemeName(signalStrengthIcon(ap.strength))
	f.ssid.SetText(ap.ssid)
	f.security.SetText(securityLabel(ap.security))
	f.secret.reset()
	f.errLabel.SetText(errMessage)
	f.errLabel.SetVisible(errMessage != "")
	f.SetVisible(true)
	f.v.focus(f.secret.entry)
}

func (f *netPasswordForm) connect() {
	password := f.secret.entry.Text()
	f.secret.reset()
	f.v.connect(password)
}

func (f *netPasswordForm) cancel() {
	f.secret.reset()
	f.v.state, f.v.selection = netNormal, nil
	f.v.rebuildList()
}

// netSecretForm is SecretForm: NetworkManager's pending credential
// prompt (a VPN password, a 2FA code), answered through the agent.
type netSecretForm struct {
	*widget.Box
	v       *networkView
	name    *widget.Label
	message *widget.Label
	fields  *widget.Box
	entries []netSecretField
	showing string
}

type netSecretField struct {
	key   string
	entry *widget.Entry
}

func newNetSecretForm(v *networkView) *netSecretForm {
	f := &netSecretForm{v: v, Box: widget.NewBox(widget.Column, 8, 10)}
	f.AddClass("card", "network-password-card", "network-secret-card")
	header := widget.NewBox(widget.Row, 10, 0)
	header.AddClass("network-password-header")
	lock := widget.NewThemeIcon("ld-lock-symbolic", int(v.px*1.3))
	header.Append(iconTile(lock, "network-connection-icon vpn", ""), false)
	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("network-password-info")
	f.name = widget.NewLabel(v.font, v.px, "", 0)
	f.name.AddClass("network-password-name")
	f.name.SetEllipsize(widget.EllipsizeEnd)
	f.name.SetMaxWidthChars(24)
	info.Append(f.name, false)
	secTitle := widget.NewLabel(v.font, v.px*0.8, i18n.T("dropdown-network-secret-title"), 0)
	secTitle.AddClass("network-password-security")
	info.Append(secTitle, false)
	header.Append(info, true)
	closeIcon := widget.NewThemeIcon("ld-x-symbolic", int(v.px))
	header.Append(dropdownButton(closeIcon, "network-password-close", f.cancel), false)
	f.Append(header, false)
	f.message = widget.NewLabel(v.font, v.px*0.85, "", 0)
	f.message.AddClass("network-secret-message")
	f.message.SetWrap(true)
	f.Append(f.message, false)
	f.fields = widget.NewBox(widget.Column, 6, 0)
	f.fields.AddClass("network-secret-fields")
	f.Append(f.fields, false)
	actions := widget.NewBox(widget.Row, 6, 0)
	actions.AddClass("network-password-actions")
	actions.Append(widget.NewSpacer(0, 0), true)
	actions.Append(v.ghostText(i18n.T("dropdown-network-cancel"), "network-password-cancel", f.cancel), false)
	actions.Append(v.primaryText(i18n.T("dropdown-network-secret-submit"), "network-password-connect", f.submit), false)
	f.Append(actions, false)
	f.SetVisible(false)
	return f
}

// secretFieldLabel is label_for: the dropdown's own wording for the
// common keys, else the service's label.
func secretFieldLabel(field secrets.Field) string {
	switch field.Key {
	case "password", "passwd", "psk", "leap-password", "secret":
		return i18n.T("dropdown-network-secret-password")
	case "user", "username", "user-name":
		return i18n.T("dropdown-network-secret-username")
	case "pin":
		return i18n.T("dropdown-network-secret-pin")
	case "usergroup":
		return i18n.T("dropdown-network-secret-group")
	case "domain":
		return i18n.T("dropdown-network-secret-domain")
	case "wep-key0", "wep-key1", "wep-key2", "wep-key3":
		return i18n.T("dropdown-network-secret-wep-key")
	case "private-key":
		return i18n.T("dropdown-network-secret-private-key")
	case "private-key-password":
		return i18n.T("dropdown-network-secret-private-key-password")
	}
	return field.Label
}

// apply mirrors the agent's pending prompt: a new request rebuilds the
// fields and takes the keyboard; none hides the form.
func (f *netSecretForm) apply(req secrets.Request, asking bool) {
	if !asking {
		f.showing = ""
		f.entries = nil
		f.fields.Clear()
		f.SetVisible(false)
		return
	}
	key := req.UUID + "\x00" + req.Setting + "\x00" + req.Message
	if key == f.showing {
		return
	}
	f.showing = key
	f.name.SetText(req.Name)
	f.message.SetText(req.Message)
	f.message.SetVisible(req.Message != "")
	f.fields.Clear()
	f.entries = nil
	for _, field := range req.Fields {
		label := widget.NewLabel(f.v.font, f.v.px*0.85, secretFieldLabel(field), 0)
		label.AddClass("network-secret-label")
		f.fields.Append(label, false)
		var entry *widget.Entry
		if field.Secret {
			s := newSecretEntry(f.v.ctx, f.v.font, f.v.px)
			entry = s.entry
			f.fields.Append(s, false)
		} else {
			entry = widget.NewEntry(f.v.font, f.v.px, 0)
			entry.AddClass("network-password-input")
			f.fields.Append(entry, false)
		}
		entry.OnActivate = func(string) { f.submit() }
		f.entries = append(f.entries, netSecretField{key: field.Key, entry: entry})
	}
	f.SetVisible(true)
	if len(f.entries) > 0 {
		f.v.focus(f.entries[0].entry)
	}
}

func (f *netSecretForm) submit() {
	agent := f.agent()
	if agent == nil {
		return
	}
	values := make(map[string]string, len(f.entries))
	for _, e := range f.entries {
		values[e.key] = e.entry.Text()
	}
	agent.Submit(values)
	f.apply(secrets.Request{}, false)
}

func (f *netSecretForm) cancel() {
	if agent := f.agent(); agent != nil {
		agent.Cancel()
	}
	f.apply(secrets.Request{}, false)
}

func (f *netSecretForm) agent() secretAgent { return f.v.agent }
