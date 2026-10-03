package bar

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/portal"
	"github.com/stubbedev/wayle/service/network"
)

// fakeVPN is a scripted vpnControl.
type fakeVPN struct {
	mu       sync.Mutex
	entries  []network.VPN
	settings map[string]network.ConnectionDict
	writeErr error
	toggled  []string
	added    []fakeVPNWrite
	updated  []fakeVPNWrite
	removed  []string
}

type fakeVPNWrite struct {
	uuid, kind, name string
	values           map[string]string
}

func (f *fakeVPN) Entries() []network.VPN {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.entries)
}

func (f *fakeVPN) Toggle(_ context.Context, uuid string) error {
	f.mu.Lock()
	f.toggled = append(f.toggled, uuid)
	f.mu.Unlock()
	return nil
}

func (f *fakeVPN) Add(_ context.Context, kind, name string, values map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, fakeVPNWrite{"", kind, name, maps.Clone(values)})
	return f.writeErr
}

func (f *fakeVPN) Update(_ context.Context, uuid, kind, name string, values map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = append(f.updated, fakeVPNWrite{uuid, kind, name, maps.Clone(values)})
	return f.writeErr
}

func (f *fakeVPN) Remove(_ context.Context, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, uuid)
	return f.writeErr
}

func (f *fakeVPN) SettingsOf(_ context.Context, uuid string) (network.ConnectionDict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dict, ok := f.settings[uuid]
	if !ok {
		return nil, errors.New("no such profile")
	}
	return dict, nil
}

func (f *fakeVPN) writes() (added, updated []fakeVPNWrite, removed, toggled []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.added), slices.Clone(f.updated), slices.Clone(f.removed), slices.Clone(f.toggled)
}

const testPlugin = "org.example.vpn"

// testKinds is WireGuard, a typed plugin with a picker, and an untyped
// plugin.
func testKinds() []network.VPNKind {
	return []network.VPNKind{
		network.AvailableKinds()[0],
		{ID: testPlugin, Label: "Example", Fields: []network.VPNField{
			{Key: "gateway", Label: "Gateway", Required: true, Format: network.FormatHost, Section: "gateway"},
			{Key: "protocol", Label: "Protocol", Choices: []network.VPNChoice{
				{Value: "anyconnect", Label: "AnyConnect", NativeSignIn: true},
				{Value: "gp", Label: "GlobalProtect"},
			}},
			{Key: "password", Label: "Password", Secret: true},
		}},
		{ID: "org.example.raw", Label: "Raw"},
	}
}

func newVPNTestView(t *testing.T, vpn *fakeVPN, open fileOpener) (*networkView, *fakePopover) {
	t.Helper()
	deps := netDeps{vpnKinds: testKinds, openFile: open}
	if vpn != nil {
		deps.vpn = vpn
		deps.changes = nil
	}
	v, _, pop := newNetTestView(t, network.Snapshot{}, deps)
	return v, pop
}

// typedEntry is the form's box for key.
func typedEntry(t *testing.T, f *netVPNForm, key string) *widget.Entry {
	t.Helper()
	for _, e := range f.entries {
		if e.key == key {
			return e.entry
		}
	}
	t.Fatalf("no box for %q", key)
	return nil
}

func TestVPNRowCaptionAndGlyph(t *testing.T) {
	failed := network.VPN{State: network.VPNFailed, Detail: "authentication failed"}
	if got := vpnRowCaption(failed); got != "Authentication failed" {
		t.Errorf("caption = %q", got)
	}
	gateway := network.VPN{State: network.VPNFailed, Detail: "  HTTP 403 from gateway "}
	if got := vpnRowCaption(gateway); got != "HTTP 403 from gateway" {
		t.Errorf("the gateway's own wording was changed: %q", got)
	}
	for state, id := range map[network.VPNState]string{
		network.VPNConnected:    "dropdown-network-vpn-connected",
		network.VPNConnecting:   "dropdown-network-vpn-connecting",
		network.VPNDisconnected: "dropdown-network-vpn-disconnected",
		network.VPNFailed:       "dropdown-network-vpn-failed",
	} {
		if got := vpnRowCaption(network.VPN{State: state}); got != i18n.T(id) {
			t.Errorf("%v caption = %q, want %q", state, got, i18n.T(id))
		}
	}
	for state, want := range map[network.VPNState]string{
		network.VPNConnected:    "ld-lock-symbolic",
		network.VPNConnecting:   "ld-refresh-cw-symbolic",
		network.VPNDisconnected: "ld-unplug-symbolic",
		network.VPNFailed:       "ld-unplug-symbolic",
	} {
		if got := vpnStateIcon(state); got != want {
			t.Errorf("%v icon = %q, want %q", state, got, want)
		}
	}
	if sentence("ärger am gateway") != "Ärger am gateway" || sentence("   ") != "" {
		t.Error("sentence touches more than the first letter")
	}
}

func TestVPNRawEditor(t *testing.T) {
	got := parseRaw("gateway = vpn.example.com\n  protocol=gp  \n\n# a comment = not a key\nno-equals\n = orphan\ntoken = abc=def==\n")
	want := map[string]string{"gateway": "vpn.example.com", "protocol": "gp", "token": "abc=def=="}
	if !maps.Equal(got, want) {
		t.Errorf("parseRaw = %v, want %v (comments, junk and keyless lines ignored)", got, want)
	}
	if back := parseRaw(renderRaw(want)); !maps.Equal(back, want) {
		t.Errorf("round trip = %v", back)
	}
	if renderRaw(map[string]string{"b": "2", "a": "1"}) != "a = 1\nb = 2" {
		t.Error("render order is not stable")
	}
}

func TestVPNMergeAndLeftovers(t *testing.T) {
	raw := map[string]string{"gateway": "raw.example.com", "extra": "x", "blank": "  ", "cleared": "old"}
	got := mergeVPNValues(raw, []vpnTyped{{"gateway", "typed.example.com"}, {"cleared", ""}})
	want := map[string]string{"gateway": "typed.example.com", "extra": "x"}
	if !maps.Equal(got, want) {
		t.Errorf("merge = %v, want %v (typed wins, emptied clears, blank dropped)", got, want)
	}
	if raw["gateway"] != "raw.example.com" {
		t.Error("merge wrote into the raw map")
	}
	kind := testKinds()[1]
	left := vpnLeftovers(map[string]string{"gateway": "g", "protocol": "gp", "mtu": "1400"}, kind)
	if !maps.Equal(left, map[string]string{"mtu": "1400"}) {
		t.Errorf("leftovers = %v, want only the key the form has no box for", left)
	}
	if len(vpnLeftovers(map[string]string{"gateway": "g"}, kind)) != 0 {
		t.Error("a profile that fits its form left raw keys")
	}
}

func TestVPNValidation(t *testing.T) {
	wg := network.AvailableKinds()[0]
	missing := vpnMissingRequired(wg, map[string]string{"interface": "wg0"})
	var keys []string
	for _, f := range missing {
		keys = append(keys, f.Key)
	}
	if !slices.Equal(keys, []string{"private-key", "address", "peer-public-key", "peer-endpoint", "peer-allowed-ips"}) {
		t.Errorf("missing = %v, want every required field at once, in order", keys)
	}
	key := network.GenerateKeyPair()
	good := map[string]string{
		"interface": "wg0", "private-key": key.Private, "address": "10.0.0.2/24",
		"peer-public-key": key.Public, "peer-endpoint": "vpn.example.com:51820", "peer-allowed-ips": "0.0.0.0/0",
	}
	if len(vpnMissingRequired(wg, good)) != 0 || len(vpnMalformed(wg, good)) != 0 {
		t.Error("a complete, well-formed profile was refused")
	}
	bad := maps.Clone(good)
	bad["peer-endpoint"] = "https://vpn.example.com"
	bad["peer-keepalive"] = "soon"
	malformed := vpnMalformed(wg, bad)
	if len(malformed) != 2 || malformed[0].Key != "peer-endpoint" || malformed[1].Key != "peer-keepalive" {
		t.Errorf("malformed = %v", malformed)
	}
	if got := vpnMalformedMessage(malformed[0]); got != i18n.T("dropdown-network-vpn-invalid-host-port", i18n.Str("field", vpnFieldName(malformed[0]))) {
		t.Errorf("message = %q", got)
	}
	if len(vpnMalformed(wg, map[string]string{"interface": "wg0"})) != 0 {
		t.Error("an absent value is malformed rather than missing")
	}
}

func TestVPNLabels(t *testing.T) {
	if got := vpnSlug("IPSec gateway"); got != "ipsec-gateway" {
		t.Errorf("slug = %q", got)
	}
	if got := vpnSlug("--Xauth  password!"); got != "xauth-password" {
		t.Errorf("slug = %q", got)
	}
	unknown := network.VPNField{Key: "zz-unknown-key", Label: "Plugin's own"}
	if vpnFieldName(unknown) != "Plugin's own" || vpnFieldLabel(unknown) != "Plugin's own" {
		t.Error("an unknown key lost the kind's own label")
	}
	unknown.Required = true
	if vpnFieldLabel(unknown) != "Plugin's own *" {
		t.Errorf("required label = %q", vpnFieldLabel(unknown))
	}
	if got := vpnSectionHeading("peer"); got != i18n.T("dropdown-network-vpn-section-peer") {
		t.Errorf("heading = %q", got)
	}
	if vpnSectionHeading("zz-made-up") != "zz-made-up" {
		t.Error("an unknown section is not shown as itself")
	}
	if vpnChoiceLabel(network.VPNChoice{Label: "AnyConnect", NativeSignIn: true}) != "AnyConnect" {
		t.Error("a native choice was annotated")
	}
	if got := vpnChoiceLabel(network.VPNChoice{Label: "GP"}); got != "GP — "+i18n.T("dropdown-network-vpn-no-native-sign-in") {
		t.Errorf("plugin choice = %q", got)
	}
	if !strings.Contains(vpnRawHint(testKinds()[1], true), "gateway, protocol, password") {
		t.Error("the typed hint does not list the covered keys")
	}
	if !strings.Contains(vpnRawHint(testKinds()[2], true), "org.example.raw") {
		t.Error("the untyped hint does not name the plugin")
	}
	pair := network.GenerateKeyPair()
	if publicKeyReadout(pair.Private) != i18n.T("dropdown-network-vpn-public-key", i18n.Str("key", pair.Public)) {
		t.Error("readout is not the derived public key")
	}
	if publicKeyReadout(pair.Private[:10]) != i18n.T("dropdown-network-vpn-public-key-empty") {
		t.Error("a half-typed key showed a public key")
	}
}

func TestKindsWith(t *testing.T) {
	kinds := testKinds()
	if len(kindsWith(kinds, testPlugin)) != len(kinds) || len(kindsWith(kinds, "")) != len(kinds) {
		t.Error("a known kind or none was added again")
	}
	got := kindsWith(kinds, "org.gone.vpn")
	if len(got) != len(kinds)+1 || got[len(kinds)].ID != "org.gone.vpn" || got[len(kinds)].IsTyped() {
		t.Errorf("uninstalled kind = %+v, want it appended, untyped", got)
	}
	if len(kinds) != 3 {
		t.Error("kindsWith changed its input")
	}
}

func TestVPNSection(t *testing.T) {
	if v, _ := newVPNTestView(t, nil, nil); v.vpns.Visible() {
		t.Error("a VPN section without NetworkManager's VPNs")
	}
	vpn := &fakeVPN{}
	v, _ := newVPNTestView(t, vpn, nil)
	if !v.vpns.Visible() || len(v.vpns.rows.Children()) != 0 || v.vpns.add.HasClass("separated") {
		t.Error("no profiles: want the section with only the add row, unseparated")
	}
	vpn.entries = []network.VPN{
		{UUID: "u1", Name: "Work", State: network.VPNConnected},
		{UUID: "u2", Name: "Home", State: network.VPNFailed, Detail: "wrong username or password"},
	}
	v.apply(v.read(context.Background()))
	if len(v.vpns.rows.Children()) != 2 || !v.vpns.add.HasClass("separated") {
		t.Fatalf("rows = %d", len(v.vpns.rows.Children()))
	}
	v.vpnToggle("u2")
	waitHeadless(t, "the toggle", func() bool { _, _, _, tg := vpn.writes(); return slices.Equal(tg, []string{"u2"}) })
}

// The VPN rows are plain boxes carrying the whole row's click
// (vpn_item.rs:82-197): not buttons, pointer cursor, and the row's
// state glyph — a plain icon — toggles the tunnel. The add row is the
// same kind of box (vpn_connections/mod.rs:75-107).
func TestVPNRowsAreClickableBoxes(t *testing.T) {
	vpn := &fakeVPN{entries: []network.VPN{{UUID: "u1", Name: "Work", State: network.VPNDisconnected}}}
	v, _ := newVPNTestView(t, vpn, nil)
	v.apply(v.read(context.Background()))
	row, ok := v.vpns.rows.Children()[0].(*widget.Box)
	if !ok {
		t.Fatalf("row = %T, want a plain box", v.vpns.rows.Children()[0])
	}
	if _, isButton := any(row).(*widget.Button); isButton {
		t.Fatal("the row is a button element")
	}
	if !row.HasClass("network-item") || !row.HasClass("vpn-item") || widget.HasClass(row, "vpn-item-toggle") {
		t.Error("the row is not the network-item vpn-item box")
	}
	if widget.CursorNameOf(row) != "pointer" {
		t.Error("the row does not carry the pointer cursor")
	}
	var icon widget.Widget
	walkTree(row, func(w widget.Widget) bool {
		if i, ok := w.(*widget.Icon); ok && i.HasClass("network-item-signal") {
			icon = i
		}
		return icon == nil
	})
	if icon == nil {
		t.Fatal("no state glyph in the row")
	}
	clickAt(t, row, icon)
	waitHeadless(t, "the row toggle", func() bool { _, _, _, tg := vpn.writes(); return slices.Equal(tg, []string{"u1"}) })

	// The add row: a box, not a button; clicking it opens the editor.
	if _, isButton := any(v.vpns.add).(*widget.Button); isButton {
		t.Fatal("the add row is a button element")
	}
	if widget.CursorNameOf(v.vpns.add) != "pointer" {
		t.Error("the add row does not carry the pointer cursor")
	}
	clickAt(t, v.vpns.add, v.vpns.add)
	if v.body.Visible() != netPageEdit {
		t.Error("clicking the add row did not open the editor")
	}
}

func TestVPNFormCreates(t *testing.T) {
	vpn := &fakeVPN{}
	v, pop := newVPNTestView(t, vpn, nil)
	f := v.vpnForm
	v.vpnAdd()
	if v.body.Visible() != netPageEdit || pop.focused != f.name {
		t.Fatalf("page %q focus %v", v.body.Visible(), pop.focused)
	}
	if !f.kindSlot.Visible() || f.deleteBtn.Visible() || !f.importBtn.Visible() || f.title.Text() != i18n.T("dropdown-network-vpn-new") {
		t.Error("creating: want the type picker and import, no delete")
	}
	// No name: refused, nothing written.
	f.save()
	if !f.errLabel.Visible() || f.errLabel.Text() != i18n.T("dropdown-network-vpn-name-required") || v.body.Visible() != netPageEdit {
		t.Errorf("nameless save: %q", f.errLabel.Text())
	}
	// The typed plugin: a missing gateway is named and marked.
	f.kindPicker.SetSelected(1)
	if len(f.entries) != 2 || len(f.pickers) != 1 || f.advanced.Visible() != true || f.raw.Visible() {
		t.Fatalf("typed kind: entries %d pickers %d", len(f.entries), len(f.pickers))
	}
	if f.importBtn.Visible() {
		t.Error("import offered for a kind that is not WireGuard")
	}
	f.name.SetText("Corp")
	f.save()
	gateway := typedEntry(t, f, "gateway")
	if f.errLabel.Text() != i18n.T("dropdown-network-vpn-field-required", i18n.Str("field", "Gateway")) || !gateway.HasClass("error") {
		t.Errorf("missing gateway: %q marked %v", f.errLabel.Text(), gateway.HasClass("error"))
	}
	if typedEntry(t, f, "password").HasClass("error") {
		t.Error("an optional box was marked")
	}
	// A URL is not a host: refused with the format's message.
	gateway.SetText("https://vpn.example.com/")
	if gateway.HasClass("error") {
		t.Error("editing a box kept its error mark")
	}
	f.save()
	if !strings.Contains(f.errLabel.Text(), "https://") || !gateway.HasClass("error") {
		t.Errorf("malformed: %q", f.errLabel.Text())
	}
	if a, _, _, _ := vpn.writes(); len(a) != 0 {
		t.Fatal("a refused form was written")
	}
	// Valid, with an advanced key and the plugin choice: written.
	gateway.SetText("vpn.example.com")
	f.pickers[0].picker.SetSelected(1)
	f.toggleAdvanced()
	if !f.raw.Visible() || f.advLabel.Text() != i18n.T("dropdown-network-vpn-advanced-hide") {
		t.Error("the advanced toggle did not open the raw editor")
	}
	f.raw.SetText("mtu = 1400\ngateway = ignored.example.com")
	f.save()
	if v.body.Visible() != netPageBrowse || f.errLabel.Visible() {
		t.Error("a valid save did not go back")
	}
	waitHeadless(t, "the add", func() bool { a, _, _, _ := vpn.writes(); return len(a) == 1 })
	a, u, _, _ := vpn.writes()
	want := map[string]string{"gateway": "vpn.example.com", "protocol": "gp", "mtu": "1400"}
	if a[0].kind != testPlugin || a[0].name != "Corp" || !maps.Equal(a[0].values, want) || len(u) != 0 {
		t.Errorf("added %+v, want %v", a[0], want)
	}
}

func TestVPNFormUntypedKind(t *testing.T) {
	vpn := &fakeVPN{}
	v, _ := newVPNTestView(t, vpn, nil)
	f := v.vpnForm
	v.vpnAdd()
	f.kindPicker.SetSelected(2)
	if len(f.entries) != 0 || f.advanced.Visible() || !f.raw.Visible() || !f.rawHint.Visible() {
		t.Error("untyped kind: want the raw editor open, no toggle")
	}
	f.name.SetText("Odd")
	f.raw.SetText("remote = 1.2.3.4")
	f.save()
	waitHeadless(t, "the add", func() bool { a, _, _, _ := vpn.writes(); return len(a) == 1 })
	if a, _, _, _ := vpn.writes(); a[0].kind != "org.example.raw" || a[0].values["remote"] != "1.2.3.4" {
		t.Errorf("added %+v", a[0])
	}
}

func TestVPNFormEditsAndDeletes(t *testing.T) {
	saved := network.BuildProfile(testPlugin, "Corp", "u1", map[string]string{"gateway": "vpn.example.com", "protocol": "gp", "mtu": "1400"})
	gone := network.BuildProfile("org.gone.vpn", "Old", "u2", map[string]string{"remote": "x"})
	vpn := &fakeVPN{
		entries:  []network.VPN{{UUID: "u1", Name: "Corp"}, {UUID: "u2", Name: "Old"}},
		settings: map[string]network.ConnectionDict{"u1": saved, "u2": gone},
	}
	v, _ := newVPNTestView(t, vpn, nil)
	f := v.vpnForm
	v.vpnEdit("u1")
	waitHeadless(t, "the edit page", func() bool { return v.body.Visible() == netPageEdit })
	if f.kindSlot.Visible() || !f.deleteBtn.Visible() || f.name.Text() != "Corp" || f.title.Text() != i18n.T("dropdown-network-vpn-new") {
		t.Error("editing: want no type picker, delete, the name, the shared New VPN title")
	}
	if typedEntry(t, f, "gateway").Text() != "vpn.example.com" || f.pickers[0].picker.Selected() != 1 {
		t.Error("the form does not hold the saved values")
	}
	if !f.raw.Visible() || f.raw.Text() != "mtu = 1400" {
		t.Errorf("raw = %q: the key the form has no box for must show", f.raw.Text())
	}
	// NM refuses: the form comes back, values intact.
	vpn.writeErr = errors.New("permission denied")
	f.save()
	waitHeadless(t, "the refusal", func() bool { return v.body.Visible() == netPageEdit && f.errLabel.Visible() })
	if f.errLabel.Text() != "permission denied" || typedEntry(t, f, "gateway").Text() != "vpn.example.com" {
		t.Errorf("refused: %q", f.errLabel.Text())
	}
	vpn.mu.Lock()
	vpn.writeErr = nil
	vpn.mu.Unlock()
	_, u, _, _ := vpn.writes()
	if len(u) != 1 || u[0].uuid != "u1" || u[0].kind != testPlugin || u[0].values["mtu"] != "1400" {
		t.Errorf("updated %+v: an edit must keep its UUID, kind and raw keys", u)
	}

	// Delete asks first; dismissing removes nothing.
	f.askDelete()
	if !f.confirm.Visible() || f.actions.Visible() || !strings.Contains(f.confirmMsg.Text(), "Corp") {
		t.Error("delete did not ask")
	}
	f.dismissDelete()
	if f.confirm.Visible() || !f.actions.Visible() {
		t.Error("dismiss kept the question")
	}
	f.confirmDelete()
	if _, _, r, _ := vpn.writes(); len(r) != 0 || v.body.Visible() != netPageEdit {
		t.Fatal("an unconfirmed delete removed the profile")
	}
	f.askDelete()
	f.confirmDelete()
	waitHeadless(t, "the remove", func() bool { _, _, r, _ := vpn.writes(); return slices.Equal(r, []string{"u1"}) })
	if v.body.Visible() != netPageBrowse {
		t.Error("delete did not go back")
	}

	// A profile whose plugin is gone opens raw and keeps its type.
	v.vpnEdit("u2")
	waitHeadless(t, "the second edit", func() bool { return v.body.Visible() == netPageEdit && f.editing == "u2" })
	if k, _ := f.kind(); k.ID != "org.gone.vpn" || f.raw.Text() != "remote = x" {
		t.Errorf("kind %q raw %q", k.ID, f.raw.Text())
	}
	f.save()
	waitHeadless(t, "the update", func() bool { _, u, _, _ := vpn.writes(); return len(u) == 2 })
	if _, u, _, _ := vpn.writes(); u[1].kind != "org.gone.vpn" {
		t.Errorf("saved as %q, not its own kind", u[1].kind)
	}

	// An unreadable profile never opens the editor, and nothing appears
	// in the list: the reason is the log's (Rust's SettingsOf failure
	// only warns).
	v.vpnCloseForm()
	v.vpnEdit("missing")
	time.Sleep(50 * time.Millisecond)
	onHeadlessLoop(func() bool { return true })
	if v.body.Visible() != netPageBrowse {
		t.Error("an unreadable profile opened the editor")
	}
}

func TestVPNFormWireGuard(t *testing.T) {
	dir := t.TempDir()
	pair, peer := network.GenerateKeyPair(), network.GenerateKeyPair()
	good := filepath.Join(dir, "office.conf")
	conf := "[Interface]\nPrivateKey = " + pair.Private + "\nAddress = 10.0.0.2/24\n\n[Peer]\nPublicKey = " + peer.Public + "\nEndpoint = vpn.example.com:51820\nAllowedIPs = 0.0.0.0/0\n"
	if err := os.WriteFile(good, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "notes.conf")
	if err := os.WriteFile(bad, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	next, nextErr := good, error(nil)
	open := func(context.Context, string, ...portal.FileFilter) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return next, nextErr
	}
	v, _ := newVPNTestView(t, &fakeVPN{}, open)
	f := v.vpnForm
	v.vpnAdd()
	// The private key's generate button and live public key.
	priv := typedEntry(t, f, wireGuardPrivateKey)
	readout := f.fields.Children()
	var pub *widget.Label
	for _, w := range readout {
		if l, ok := w.(*widget.Label); ok && l.HasClass("network-vpn-public-key") {
			pub = l
		}
	}
	if pub == nil || pub.Text() != i18n.T("dropdown-network-vpn-public-key-empty") {
		t.Fatal("no empty public-key readout")
	}
	priv.SetText(pair.Private)
	if pub.Text() != publicKeyReadout(pair.Private) {
		t.Error("the readout does not follow the private key")
	}
	priv.SetText("not a key")
	if pub.Text() != i18n.T("dropdown-network-vpn-public-key-empty") {
		t.Error("a broken key kept the old public key")
	}

	// Import fills the form and names it after the interface.
	f.importFile()
	waitHeadless(t, "the import", func() bool { return f.name.Text() == "office" })
	if typedEntry(t, f, "peer-endpoint").Text() != "vpn.example.com:51820" || typedEntry(t, f, wireGuardPrivateKey).Text() != pair.Private {
		t.Error("import did not fill the fields")
	}
	// A typed name is kept; a file that is no config says so.
	f.name.SetText("Mine")
	mu.Lock()
	next = bad
	mu.Unlock()
	f.importFile()
	waitHeadless(t, "the import failure", func() bool { return f.errLabel.Visible() })
	if f.errLabel.Text() != i18n.T("dropdown-network-vpn-import-failed") || f.name.Text() != "Mine" {
		t.Errorf("bad file: %q name %q", f.errLabel.Text(), f.name.Text())
	}
	// Dismissing the chooser is no failure, nor is a portal error one
	// the form can explain.
	f.setError("")
	f.importDone("", fmt.Errorf("open: %w", portal.ErrCancelled))
	f.importDone("", errors.New("no portal"))
	if f.errLabel.Visible() || f.name.Text() != "Mine" {
		t.Error("a dismissed chooser reported a failure")
	}
}
