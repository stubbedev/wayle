package network

import (
	"context"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/network/openconnect"
	"github.com/stubbedev/wayle/service/network/secrets"
)

var fastResume = resumeTiming{
	networkWait:  2 * time.Second,
	attempts:     2,
	retryDelay:   50 * time.Millisecond,
	nmStopWindow: 20 * time.Second,
	agentSettle:  50 * time.Millisecond,
}

type serviceFixture struct {
	nm     *fakeNM
	svc    *Service
	signIn *fakeSignIn
	conn   *dbus.Conn
}

func startService(t *testing.T, setup func(f *fakeNM)) *serviceFixture {
	t.Helper()
	f := startFakeNM(t)
	if setup != nil {
		setup(f)
	}
	conn := f.client()
	signIn := &fakeSignIn{}
	svc, err := start(t.Context(), conn, signIn, RequestBudget, fastResume)
	if err != nil {
		t.Fatal(err)
	}
	f.registered(t)
	return &serviceFixture{nm: f, svc: svc, signIn: signIn, conn: conn}
}

func (fx *serviceFixture) row(t *testing.T, uuid string) VPN {
	t.Helper()
	row, ok := fx.svc.VPN.Get(uuid)
	if !ok {
		t.Fatalf("no row for %s in %+v", uuid, fx.svc.VPN.Entries())
	}
	return row
}

func (fx *serviceFixture) waitRow(t *testing.T, uuid, what string, cond func(VPN) bool) {
	t.Helper()
	waitFor(t, what, func() bool {
		row, ok := fx.svc.VPN.Get(uuid)
		return ok && cond(row)
	})
}

func (fx *serviceFixture) activations() []dbus.ObjectPath {
	fx.nm.mu.Lock()
	defer fx.nm.mu.Unlock()
	return slices.Clone(fx.nm.activations)
}

func (fx *serviceFixture) activeFor(t *testing.T, uuid string) dbus.ObjectPath {
	t.Helper()
	var path dbus.ObjectPath
	waitFor(t, "an active connection for "+uuid, func() bool {
		fx.nm.mu.Lock()
		defer fx.nm.mu.Unlock()
		for p, a := range fx.nm.active {
			if a.uuid == uuid {
				path = p
				return true
			}
		}
		return false
	})
	return path
}

func withProfiles(dicts ...ConnectionDict) func(f *fakeNM) {
	return func(f *fakeNM) {
		for _, d := range dicts {
			f.addProfile(d)
		}
	}
}

func wgConnection(uuid, name string) ConnectionDict {
	return ConnectionDict{"connection": connectionSection(uuid, name, WireGuard)}
}

func TestTheVPNListIsWhateverNetworkManagerHolds(t *testing.T) {
	fx := startService(t, withProfiles(
		gpConnection("work"),
		wifiConnection("home-wifi", "Home", []byte("home")),
		wgConnection("tunnel", "Tunnel"),
	))
	rows := fx.svc.VPN.Entries()
	if len(rows) != 2 || rows[0].UUID != "work" || rows[1].UUID != "tunnel" {
		t.Fatalf("rows = %+v; want the VPN and the WireGuard profile, not the wifi", rows)
	}
	if rows[0].WireGuard || !rows[1].WireGuard {
		t.Errorf("wireguard flags = %v %v", rows[0].WireGuard, rows[1].WireGuard)
	}

	added := fx.nm.addProfile(formVPNConnection("office"))
	waitFor(t, "the new profile", func() bool { _, ok := fx.svc.VPN.Get("office"); return ok })

	fx.nm.removeProfile(added)
	waitFor(t, "the removed profile to go", func() bool { _, ok := fx.svc.VPN.Get("office"); return !ok })
	if fx.svc.VPN.IsEmpty() {
		t.Error("the list emptied with two VPNs left")
	}
}

func TestARenameLandsOnTheRow(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	renamed := gpConnection("work")
	renamed["connection"]["id"] = dbus.MakeVariant("Office VPN")
	if err := (fakeConnection{f: fx.nm, path: settingPath(1)}).Update(renamed); err != nil {
		t.Fatal(err)
	}
	fx.waitRow(t, "work", "the rename", func(r VPN) bool { return r.Name == "Office VPN" })
}

func TestConnectFollowsNetworkManagerUpAndDown(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	ctx := t.Context()
	if err := fx.svc.VPN.Connect(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if got := fx.activations(); !slices.Equal(got, []dbus.ObjectPath{settingPath(1)}) {
		t.Errorf("activations = %v", got)
	}
	if row := fx.row(t, "work"); row.State != VPNConnecting {
		t.Errorf("state after connect = %v, want the activating object's connecting", row.State)
	}
	active := fx.activeFor(t, "work")
	fx.waitRow(t, "work", "the resync to watch the new object", func(VPN) bool {
		fx.svc.VPN.mu.Lock()
		defer fx.svc.VPN.mu.Unlock()
		return fx.svc.VPN.watched[active] != nil
	})
	fx.nm.setActiveState(active, ActiveActivated, ReasonNone)
	fx.waitRow(t, "work", "connected", func(r VPN) bool { return r.State == VPNConnected })
	if fx.svc.VPN.Aggregate() != VPNConnected {
		t.Errorf("aggregate = %v", fx.svc.VPN.Aggregate())
	}
	// Connecting a connected tunnel does nothing.
	must(t, fx.svc.VPN.Connect(ctx, "work"))
	if len(fx.activations()) != 1 {
		t.Error("an up tunnel was activated again")
	}

	must(t, fx.svc.VPN.Disconnect(ctx, "work"))
	fx.waitRow(t, "work", "disconnected", func(r VPN) bool { return r.State == VPNDisconnected })
	if row := fx.row(t, "work"); row.Detail != "" {
		t.Errorf("a user disconnect was captioned %q", row.Detail)
	}
	// Already down is not an error.
	must(t, fx.svc.VPN.Disconnect(ctx, "work"))
}

func TestAFailedAttemptKeepsItsReason(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	must(t, fx.svc.VPN.Connect(t.Context(), "work"))
	active := fx.activeFor(t, "work")
	fx.waitRow(t, "work", "the watch", func(VPN) bool {
		fx.svc.VPN.mu.Lock()
		defer fx.svc.VPN.mu.Unlock()
		return fx.svc.VPN.watched[active] != nil
	})
	fx.nm.setActiveState(active, ActiveDeactivated, ReasonLoginFailed)
	fx.nm.dropActive(active)
	fx.waitRow(t, "work", "the failure", func(r VPN) bool { return r.State == VPNFailed })
	time.Sleep(50 * time.Millisecond)
	if row := fx.row(t, "work"); row.State != VPNFailed || row.Detail != "authentication failed" {
		t.Errorf("row = %+v; the active list emptying must not wipe the reason", row)
	}
	if fx.svc.VPN.Aggregate() != VPNFailed {
		t.Errorf("aggregate = %v", fx.svc.VPN.Aggregate())
	}
}

func TestTheGatewaysOwnReasonOutranksNetworkManagersGenericOne(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	fx.signIn.authenticate = func(context.Context, openconnect.Profile, bool, bool, secrets.Prompter) (map[string]string, error) {
		return nil, &secrets.AuthenticationFailedError{Reason: "Invalid username or password"}
	}
	must(t, fx.svc.VPN.Connect(t.Context(), "work"))
	active := fx.activeFor(t, "work")
	// NM asks the agent, which fails the sign-in and publishes why.
	_, _ = fx.nm.getSecrets(t.Context(), fx.conn.Names()[0], gpConnection("work"), settingPath(1), "vpn", nil, flagAllowInteraction)
	fx.waitRow(t, "work", "the gateway's reason", func(r VPN) bool { return r.Detail == "Invalid username or password" })
	fx.waitRow(t, "work", "the watch", func(VPN) bool {
		fx.svc.VPN.mu.Lock()
		defer fx.svc.VPN.mu.Unlock()
		return fx.svc.VPN.watched[active] != nil
	})
	fx.nm.setActiveState(active, ActiveDeactivated, ReasonNoSecrets)
	time.Sleep(50 * time.Millisecond)
	if row := fx.row(t, "work"); row.Detail != "Invalid username or password" || row.State != VPNFailed {
		t.Errorf("row = %+v; NM's generic caption replaced the gateway's", row)
	}
}

func TestARefusedActivationFailsTheRowWithNMsWords(t *testing.T) {
	fx := startService(t, withProfiles(formVPNConnection("office")))
	fx.nm.mu.Lock()
	fx.nm.activateErr = dbus.NewError("org.freedesktop.NetworkManager.MissingPlugin", []any{"the VPN plugin is not installed"})
	fx.nm.mu.Unlock()
	err := fx.svc.VPN.Connect(t.Context(), "office")
	if err == nil || !strings.Contains(err.Error(), "the VPN plugin is not installed") {
		t.Fatalf("err = %v", err)
	}
	row := fx.row(t, "office")
	if row.State != VPNFailed || !strings.Contains(row.Detail, "the VPN plugin is not installed") {
		t.Errorf("row = %+v", row)
	}
	// A refusal leaves no unattended mark behind for a later click.
	if fx.svc.Agent.takeUnattended("office", time.Now()) {
		t.Error("a refused restore left its mark")
	}
}

func TestToggleConnectsWhenDownAndCancelsWhenConnecting(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	ctx := t.Context()
	must(t, fx.svc.VPN.Toggle(ctx, "work"))
	active := fx.activeFor(t, "work")
	if fx.row(t, "work").State != VPNConnecting {
		t.Fatal("toggle did not connect")
	}
	must(t, fx.svc.VPN.Toggle(ctx, "work"))
	fx.nm.mu.Lock()
	deactivated := slices.Clone(fx.nm.deactivations)
	fx.nm.mu.Unlock()
	if !slices.Equal(deactivated, []dbus.ObjectPath{active}) {
		t.Errorf("deactivations = %v; a connecting row toggles off", deactivated)
	}
	if err := fx.svc.VPN.Toggle(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "no VPN profile with uuid nope") {
		t.Errorf("unknown uuid: err = %v", err)
	}
}

func TestAnUnrelatedProfileChangeLeavesAConnectedRowAlone(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	must(t, fx.svc.VPN.Connect(t.Context(), "work"))
	active := fx.activeFor(t, "work")
	fx.waitRow(t, "work", "the watch", func(VPN) bool {
		fx.svc.VPN.mu.Lock()
		defer fx.svc.VPN.mu.Unlock()
		return fx.svc.VPN.watched[active] != nil
	})
	fx.nm.setActiveState(active, ActiveActivated, ReasonNone)
	fx.waitRow(t, "work", "connected", func(r VPN) bool { return r.State == VPNConnected })
	fx.nm.addProfile(wifiConnection("cafe", "Cafe", []byte("cafe")))
	time.Sleep(100 * time.Millisecond)
	if row := fx.row(t, "work"); row.State != VPNConnected {
		t.Errorf("row = %+v after a wifi profile was added", row)
	}
	// And its transitions still arrive.
	fx.nm.setActiveState(active, ActiveDeactivated, ReasonDeviceDisconnected)
	fx.waitRow(t, "work", "the drop", func(r VPN) bool {
		return r.State == VPNFailed && r.Detail == "the underlying network went away"
	})
}

func currentUser(t *testing.T) string {
	t.Helper()
	u, err := user.LookupId(strconv.Itoa(os.Geteuid()))
	if err != nil {
		t.Skipf("the test user has no passwd entry: %v", err)
	}
	return u.Username
}

func (fx *serviceFixture) lastAdded(t *testing.T) ConnectionDict {
	t.Helper()
	var dict ConnectionDict
	waitFor(t, "an AddConnection", func() bool {
		fx.nm.mu.Lock()
		defer fx.nm.mu.Unlock()
		if len(fx.nm.added) == 0 {
			return false
		}
		dict = fx.nm.added[len(fx.nm.added)-1]
		return true
	})
	return dict
}

func permissions(dict ConnectionDict) []string {
	p, _ := dict["connection"]["permissions"].Value().([]string)
	return p
}

func TestAWireGuardProfileMadeHereIsItsUsersOwn(t *testing.T) {
	me := currentUser(t)
	fx := startService(t, nil)
	values := map[string]string{
		"interface":        "wgtest0",
		"private-key":      "6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=",
		"address":          "10.123.45.2/24",
		"peer-public-key":  "Kx3AZBHm3vDJXPGRAJfvTvUEHY1c2Jw4qYE9nR6qEXY=",
		"peer-endpoint":    "vpn.invalid:51820",
		"peer-allowed-ips": "10.123.45.0/24",
	}
	must(t, fx.svc.VPN.Add(t.Context(), WireGuard, "wayle-test-wireguard", values))
	added := fx.lastAdded(t)
	if got := permissions(added); !slices.Equal(got, []string{"user:" + me + ":"}) {
		t.Errorf("permissions = %v", got)
	}
	uuid := dictReader(added).str("connection", "uuid")
	fx.waitRow(t, uuid, "the new profile in the list", func(r VPN) bool { return r.WireGuard && r.Name == "wayle-test-wireguard" })
	stored, err := fx.svc.VPN.SettingsOf(t.Context(), uuid)
	must(t, err)
	if KindOf(stored) != WireGuard || ReadProfileValues(stored)["interface"] != "wgtest0" {
		t.Errorf("stored reads back as %q %v", KindOf(stored), ReadProfileValues(stored))
	}
	must(t, fx.svc.VPN.Remove(t.Context(), uuid))
	waitFor(t, "the deleted profile to go", func() bool { _, ok := fx.svc.VPN.Get(uuid); return !ok })
	if !slices.Equal(fx.signIn.forgotten, []string{uuid}) {
		t.Errorf("forgotten = %v; a deleted profile's cache must go with it", fx.signIn.forgotten)
	}
}

func TestAnOpenconnectProfileIsSystemWideAndPersistent(t *testing.T) {
	fx := startService(t, nil)
	fx.svc.VPN.pluginDirs = nil
	must(t, fx.svc.VPN.Add(t.Context(), openconnect.ServiceType, "wayle-test-openconnect", map[string]string{
		"gateway": "vpn.invalid", "protocol": "gp", "wayle-username": "tester",
	}))
	added := fx.lastAdded(t)
	if got := permissions(added); got != nil {
		t.Errorf("permissions = %v; openconnect does not support private profiles", got)
	}
	if added["vpn"]["persistent"].Value() != true {
		t.Error("openconnect profile not persistent")
	}
	read := ReadProfileValues(added)
	if read["wayle-username"] != "tester" || read["protocol"] != "gp" {
		t.Errorf("read back = %v", read)
	}
}

func TestSecretsTypedIntoTheFormNeverLandInVPNData(t *testing.T) {
	fx := startService(t, nil)
	must(t, fx.svc.VPN.Add(t.Context(), "org.freedesktop.NetworkManager.vpnc", "Cisco", map[string]string{
		"IPSec gateway": "vpn.example.com", "IPSec ID": "staff", "IPSec secret": "group-pw", "Xauth password": "hunter2",
	}))
	added := fx.lastAdded(t)
	data := stringDict(added["vpn"]["data"])
	stored := stringDict(added["vpn"]["secrets"])
	for _, key := range []string{"IPSec secret", "Xauth password"} {
		if _, leaked := data[key]; leaked {
			t.Errorf("%q written to vpn.data", key)
		}
		if stored[key] == "" {
			t.Errorf("%q missing from vpn.secrets", key)
		}
	}
	if data["IPSec gateway"] != "vpn.example.com" {
		t.Errorf("data = %v", data)
	}
}

func TestUpdateRewritesTheProfileItIsEditing(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	must(t, fx.svc.VPN.Update(t.Context(), "work", openconnect.ServiceType, "Renamed", map[string]string{"gateway": "b.example.com", "protocol": "gp"}))
	fx.waitRow(t, "work", "the rename", func(r VPN) bool { return r.Name == "Renamed" })
	stored, _ := fx.nm.profile(settingPath(1))
	if dictReader(stored).str("connection", "uuid") != "work" || stringDict(stored["vpn"]["data"])["gateway"] != "b.example.com" {
		t.Errorf("stored = %v", stored)
	}
	if err := fx.svc.VPN.Update(t.Context(), "gone", WireGuard, "x", nil); err == nil {
		t.Error("updating a missing profile succeeded")
	}
	if err := fx.svc.VPN.Remove(t.Context(), "gone"); err == nil || len(fx.signIn.forgotten) != 0 {
		t.Errorf("removing a missing profile: err=%v forgotten=%v", err, fx.signIn.forgotten)
	}
}

func TestTheSSOCallbackReachesAWaitingSignIn(t *testing.T) {
	fx := startService(t, nil)
	if fx.svc.VPN.DeliverSSOCallback("globalprotectcallback:x") {
		t.Error("a callback with nothing waiting was taken")
	}
	fx.signIn.waiting = true
	if !fx.svc.VPN.DeliverSSOCallback("globalprotectcallback:y") {
		t.Error("the waiting sign-in did not take the callback")
	}
	if !slices.Equal(fx.signIn.callbacks, []string{"globalprotectcallback:x", "globalprotectcallback:y"}) {
		t.Errorf("callbacks = %v", fx.signIn.callbacks)
	}
}

// sleep emits logind's PrepareForSleep from its own peer.
func (fx *serviceFixture) sleep(t *testing.T, start bool) {
	t.Helper()
	logind := fx.nm.bus.Conn(t)
	must(t, logind.Emit(login1Path, login1Iface+".PrepareForSleep", start))
}

func TestATunnelUpAtSuspendIsBroughtBackUnattendedOnWake(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work"), gpConnection("idle")))
	var unattended []bool
	fx.signIn.authenticate = func(_ context.Context, _ openconnect.Profile, _, u bool, _ secrets.Prompter) (map[string]string, error) {
		unattended = append(unattended, u)
		return map[string]string{"cookie": "c"}, nil
	}
	up := fx.nm.startActive("work", ActiveActivated)
	fx.waitRow(t, "work", "connected", func(r VPN) bool { return r.State == VPNConnected })

	fx.sleep(t, true)
	time.Sleep(100 * time.Millisecond)
	fx.nm.dropActive(up)
	fx.nm.setState(StateConnectedLocal)
	fx.sleep(t, false)
	time.Sleep(150 * time.Millisecond)
	if len(fx.activations()) != 0 {
		t.Fatal("restored before the network had a default route")
	}
	fx.nm.setState(StateConnectedGlobal)
	waitFor(t, "the restore", func() bool { return len(fx.activations()) == 1 })
	if got := fx.activations(); got[0] != settingPath(1) {
		t.Errorf("restored %v; only the tunnel that was up", got)
	}
	// The activation was said to have nobody watching.
	_, _ = fx.nm.getSecrets(t.Context(), fx.conn.Names()[0], gpConnection("work"), settingPath(1), "vpn", nil, flagAllowInteraction)
	if !slices.Equal(unattended, []bool{true}) {
		t.Errorf("unattended = %v", unattended)
	}
}

func TestATunnelThatWasDownIsNotDialledOnWake(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work")))
	fx.sleep(t, true)
	time.Sleep(100 * time.Millisecond)
	fx.sleep(t, false)
	time.Sleep(200 * time.Millisecond)
	if got := fx.activations(); len(got) != 0 {
		t.Errorf("activations = %v", got)
	}
}

func TestTunnelsNetworkManagerTookWithItComeBackAndTurnedOffOnesStayOff(t *testing.T) {
	fx := startService(t, withProfiles(gpConnection("work"), gpConnection("off")))
	fx.nm.startActive("work", ActiveActivated)
	offActive := fx.nm.startActive("off", ActiveActivated)
	fx.waitRow(t, "off", "connected", func(r VPN) bool { return r.State == VPNConnected })
	fx.waitRow(t, "work", "connected", func(r VPN) bool { return r.State == VPNConnected })
	must(t, fx.svc.VPN.Disconnect(t.Context(), "off"))
	_ = offActive
	fx.waitRow(t, "off", "turned off", func(r VPN) bool { return r.State == VPNDisconnected })

	fx.nm.stop()
	fx.waitRow(t, "work", "down with NM", func(r VPN) bool { return r.State == VPNDisconnected })
	fx.nm.own()
	waitFor(t, "the restore", func() bool { return len(fx.activations()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if got := fx.activations(); !slices.Equal(got, []dbus.ObjectPath{settingPath(1)}) {
		t.Errorf("activations = %v; only the tunnel NM took", got)
	}
}

func TestVPNStatePureHelpers(t *testing.T) {
	now := time.Now()
	for _, before := range []VPNState{VPNConnected, VPNConnecting} {
		for _, after := range []VPNState{VPNDisconnected, VPNFailed} {
			if got := nextWentDownAt(before, after, time.Time{}, now); !got.Equal(now) {
				t.Errorf("%v -> %v did not stamp", before, after)
			}
		}
	}
	earlier := now.Add(-time.Second)
	if got := nextWentDownAt(VPNDisconnected, VPNFailed, earlier, now); !got.Equal(earlier) {
		t.Error("the stamp from leaving up did not survive the second step")
	}
	if !nextWentDownAt(VPNFailed, VPNConnecting, earlier, now).IsZero() {
		t.Error("coming up again kept a stale stamp")
	}
	if !nextWentDownAt(VPNDisconnected, VPNFailed, time.Time{}, now).IsZero() {
		t.Error("a sign-in that never came up was stamped as a drop")
	}

	if !nextTurnedOff(false, VPNDisconnected, ReasonUserDisconnected) || !nextTurnedOff(true, VPNFailed, ReasonServiceStopped) {
		t.Error("a deactivation someone asked for must turn the tunnel off, through the teardown")
	}
	for _, reason := range []ActiveReason{ReasonServiceStopped, ReasonDeviceDisconnected, ReasonNone} {
		if nextTurnedOff(false, VPNFailed, reason) {
			t.Errorf("%v counted as turned off", reason)
		}
	}
	if nextTurnedOff(true, VPNConnecting, ReasonUserDisconnected) {
		t.Error("coming up again stayed turned off")
	}

	for states, want := range map[string]VPNState{
		"":    VPNDisconnected,
		"dc":  VPNConnecting,
		"cC":  VPNConnected,
		"f":   VPNFailed,
		"fc":  VPNConnecting,
		"df":  VPNFailed,
		"Cfc": VPNConnected,
	} {
		var list []VPNState
		for _, c := range states {
			list = append(list, map[rune]VPNState{'d': VPNDisconnected, 'c': VPNConnecting, 'C': VPNConnected, 'f': VPNFailed}[c])
		}
		if got := foldStates(list); got != want {
			t.Errorf("fold(%q) = %v, want %v", states, got, want)
		}
	}

	if got := mergeDetail("Invalid username or password", "credentials not provided"); got != "Invalid username or password" {
		t.Errorf("merge = %q", got)
	}
	if got := mergeDetail("", "connection timed out"); got != "connection timed out" {
		t.Errorf("merge = %q", got)
	}
	if got := mergeDetail("Invalid password", ""); got != "" {
		t.Errorf("a clean change kept %q", got)
	}

	if s, d := resolveState(ActiveDeactivated, ReasonLoginFailed); s != VPNFailed || d != "authentication failed" {
		t.Errorf("login failed = %v %q", s, d)
	}
	if s, d := resolveState(ActiveDeactivated, ReasonUserDisconnected); s != VPNDisconnected || d != "" {
		t.Errorf("user disconnect = %v %q", s, d)
	}
	if s, d := resolveState(ActiveActivating, ReasonLoginFailed); s != VPNConnecting || d != "" {
		t.Errorf("stale reason on the way up = %v %q", s, d)
	}
	if reasonText(ReasonNoSecrets) != "credentials not provided" || reasonText(ReasonNone) != "" {
		t.Error("reason texts")
	}
}

func TestAGeneratedUUIDIsVersionFour(t *testing.T) {
	uuid := newUUID()
	if len(uuid) != 36 || strings.Count(uuid, "-") != 4 || uuid[14] != '4' || !strings.ContainsRune("89ab", rune(uuid[19])) {
		t.Errorf("uuid = %q", uuid)
	}
	if newUUID() == uuid {
		t.Error("two UUIDs collided")
	}
}

func TestResumePureHelpers(t *testing.T) {
	known := []string{"work", "home", "lab"}
	got := toRestore(map[string]ActiveState{"work": ActiveActivated, "home": ActiveActivating, "lab": ActiveDeactivating}, known)
	if !slices.Equal(got, []string{"work", "home", "lab"}) {
		t.Errorf("up or on its way = %v", got)
	}
	if got := toRestore(map[string]ActiveState{"work": ActiveDeactivated, "home": ActiveUnknown}, known); got != nil {
		t.Errorf("down tunnels restored: %v", got)
	}
	if got := toRestore(map[string]ActiveState{"wifi": ActiveActivated, "work": ActiveActivated}, []string{"work"}); !slices.Equal(got, []string{"work"}) {
		t.Errorf("non-VPN connections are NM's: %v", got)
	}

	if got := carriedOver([]string{"work"}, nil); !slices.Equal(got, []string{"work"}) {
		t.Errorf("carried = %v", got)
	}
	if got := carriedOver([]string{"work"}, []string{"home", "work"}); !slices.Equal(got, []string{"work", "home"}) {
		t.Errorf("carried = %v", got)
	}
	if got := carriedOver(nil, nil); len(got) != 0 {
		t.Errorf("carried = %v", got)
	}

	now := time.Now()
	ago := func(s int) time.Time { return now.Add(-time.Duration(s) * time.Second) }
	window := 20 * time.Second
	owed := owedAfterNMStop([]nmStopRow{
		{"work", VPNConnected, time.Time{}, false},
		{"home", VPNConnecting, time.Time{}, false},
		{"lab", VPNFailed, ago(4), false},
		{"dc", VPNDisconnected, ago(3), false},
	}, now, window)
	if !slices.Equal(owed, []string{"work", "home", "lab", "dc"}) {
		t.Errorf("owed = %v", owed)
	}
	owed = owedAfterNMStop([]nmStopRow{
		{"work", VPNDisconnected, ago(1), true},
		{"vpn2", VPNFailed, ago(1), true},
		{"home", VPNFailed, ago(600), false},
		{"lab", VPNFailed, time.Time{}, false},
		{"idle", VPNDisconnected, time.Time{}, false},
	}, now, window)
	if len(owed) != 0 {
		t.Errorf("owed a tunnel NM did not take down: %v", owed)
	}

	if !isNetworkBack(StateConnectedGlobal) || !isNetworkBack(StateConnectedSite) {
		t.Error("a default route is back")
	}
	for _, s := range []State{StateUnknown, StateAsleep, StateDisconnected, StateDisconnecting, StateConnecting, StateConnectedLocal} {
		if isNetworkBack(s) {
			t.Errorf("%v counted as back", s)
		}
	}
}
