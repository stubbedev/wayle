package network

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func wifiFixture(t *testing.T, setup func(f *fakeNM)) (*serviceFixture, *Wifi) {
	t.Helper()
	fx := startService(t, setup)
	if fx.svc.Wifi == nil {
		t.Fatal("no wifi device found")
	}
	if fx.svc.Wifi.Device != fakeWifiPath {
		t.Fatalf("device = %s", fx.svc.Wifi.Device)
	}
	return fx, fx.svc.Wifi
}

func TestSecurityIsTheStrongestTheAPOffers(t *testing.T) {
	for _, tc := range []struct {
		flags, wpa, rsn uint32
		want            SecurityType
	}{
		{0, 0, 0, SecurityNone},
		{apFlagPrivacy, 0, 0, SecurityWEP},
		{0, secPairWEP40, 0, SecurityWEP},
		{apFlagPrivacy, secKeyMgmtPSK, 0, SecurityWPA},
		{apFlagPrivacy, secKeyMgmtPSK, secKeyMgmtPSK, SecurityWPA2},
		{apFlagPrivacy, 0, secKeyMgmtSAE | secKeyMgmtPSK, SecurityWPA3},
		{apFlagPrivacy, 0, secKeyMgmt8021X, SecurityEnterprise},
		{apFlagPrivacy, secKeyMgmtEAPSuiteB, secKeyMgmtSAE, SecurityEnterprise},
	} {
		if got := securityFromFlags(tc.flags, tc.wpa, tc.rsn); got != tc.want {
			t.Errorf("flags %#x wpa %#x rsn %#x = %v, want %v", tc.flags, tc.wpa, tc.rsn, got, tc.want)
		}
	}
	if SecurityNone.RequiresPassword() || SecurityEnterprise.RequiresPassword() {
		t.Error("open and enterprise networks ask for no simple password")
	}
	for _, s := range []SecurityType{SecurityWEP, SecurityWPA, SecurityWPA2, SecurityWPA3} {
		if !s.RequiresPassword() {
			t.Errorf("%v needs a password", s)
		}
	}
}

func listed(ssid string, strength uint8) ListedNetwork {
	return ListedNetwork{AccessPoint{SSID: []byte(ssid), Strength: strength, Security: SecurityWPA2}, false, false}
}

func names(list []ListedNetwork) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		suffix := ""
		if n.Stale {
			suffix = "~"
		}
		out = append(out, n.Name()+suffix)
	}
	return out
}

func TestTheListIsUniqueStrongestFirstWithoutHiddenEnterpriseOrConnected(t *testing.T) {
	aps := []AccessPoint{
		{SSID: []byte("home"), Strength: 40},
		{SSID: []byte("home"), Strength: 80, Path: "/strong"},
		{SSID: []byte("cafe"), Strength: 60},
		{SSID: nil, Strength: 99},
		{SSID: []byte("corp"), Strength: 90, Security: SecurityEnterprise},
		{SSID: []byte("mine"), Strength: 70},
	}
	got := SortedUniqueNetworks(aps, "mine", map[string]bool{"cafe": true})
	if !slices.Equal(names(got), []string{"home", "cafe"}) {
		t.Fatalf("list = %v", names(got))
	}
	if got[0].Path != "/strong" || got[0].Known || !got[1].Known {
		t.Errorf("list = %+v", got)
	}
}

func TestANetworkNMPrunedStaysListedAsStale(t *testing.T) {
	cached := []ListedNetwork{listed("home", 80), listed("cafe", 40)}
	if got := names(MergeWithCache([]ListedNetwork{listed("home", 75)}, cached, "", nil)); !slices.Equal(got, []string{"home", "cafe~"}) {
		t.Errorf("merged = %v", got)
	}
	merged := MergeWithCache([]ListedNetwork{listed("home", 30)}, []ListedNetwork{listed("home", 80)}, "", nil)
	if len(merged) != 1 || merged[0].Strength != 30 || merged[0].Stale {
		t.Errorf("a live network was duplicated or kept its stale reading: %+v", merged)
	}
	if got := names(MergeWithCache([]ListedNetwork{listed("weak", 10)}, []ListedNetwork{listed("strong-but-gone", 99)}, "", nil)); !slices.Equal(got, []string{"weak", "strong-but-gone~"}) {
		t.Errorf("live first: %v", got)
	}
	if got := names(MergeWithCache(nil, cached, "home", nil)); !slices.Equal(got, []string{"cafe~"}) {
		t.Errorf("the connected network was resurrected: %v", got)
	}
	saved := listed("cafe", 40)
	saved.Known = true
	merged = MergeWithCache(nil, []ListedNetwork{saved, listed("home", 80)}, "", map[string]bool{"home": true})
	if merged[0].Known || !merged[1].Known {
		t.Errorf("known not recomputed: %+v", merged)
	}
}

func TestANewNetworkGetsAProfileWithItsKey(t *testing.T) {
	fx, w := wifiFixture(t, nil)
	ap := fx.nm.wifi.addAP(fakeAP{ssid: "cafe", strength: 70, flags: apFlagPrivacy, rsn: secKeyMgmtPSK, bssid: "AA:BB:CC:DD:EE:FF"})
	if _, err := w.Connect(t.Context(), ap, "correct horse"); err != nil {
		t.Fatal(err)
	}
	fx.nm.mu.Lock()
	dict := fx.nm.addAndActive[0]
	fx.nm.mu.Unlock()
	security := dict["802-11-wireless-security"]
	if variantString(security["key-mgmt"]) != "wpa-psk" || variantString(security["psk"]) != "correct horse" {
		t.Errorf("security = %v", security)
	}
	if _, locked := dict["802-11-wireless"]["bssid"]; locked {
		t.Error("an ordinary SSID was locked to its BSSID")
	}
	if dictReader(dict).str("connection", "id") != "cafe" {
		t.Errorf("connection = %v", dict["connection"])
	}
}

func TestAManufacturerDefaultSSIDIsLockedToItsRouter(t *testing.T) {
	fx, w := wifiFixture(t, nil)
	ap := fx.nm.wifi.addAP(fakeAP{ssid: "NETGEAR", strength: 70, bssid: "AA:BB:CC:DD:EE:FF"})
	if _, err := w.Connect(t.Context(), ap, ""); err != nil {
		t.Fatal(err)
	}
	fx.nm.mu.Lock()
	dict := fx.nm.addAndActive[0]
	fx.nm.mu.Unlock()
	if got, _ := dict["802-11-wireless"]["bssid"].Value().([]byte); !slices.Equal(got, []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}) {
		t.Errorf("bssid = %v", dict["802-11-wireless"]["bssid"])
	}
	if _, ok := dict["802-11-wireless-security"]; ok {
		t.Error("an open network got a security section")
	}
}

func TestASavedNetworkReusesItsProfileAndTakesANewPassword(t *testing.T) {
	fx, w := wifiFixture(t, withProfiles(wifiConnection("home-uuid", "Home", []byte("home"))))
	ap := fx.nm.wifi.addAP(fakeAP{ssid: "home", strength: 70, flags: apFlagPrivacy, rsn: secKeyMgmtSAE})
	if _, err := w.Connect(t.Context(), ap, "new-pass"); err != nil {
		t.Fatal(err)
	}
	fx.nm.mu.Lock()
	created := len(fx.nm.addAndActive)
	activated := slices.Clone(fx.nm.activations)
	fx.nm.mu.Unlock()
	if created != 0 || !slices.Equal(activated, []dbus.ObjectPath{settingPath(1)}) {
		t.Errorf("created %d, activated %v; the saved profile must be reused", created, activated)
	}
	stored, _ := fx.nm.profile(settingPath(1))
	if sec := stored["802-11-wireless-security"]; variantString(sec["key-mgmt"]) != "sae" || variantString(sec["psk"]) != "new-pass" {
		t.Errorf("stored security = %v", sec)
	}
	// Without a password the saved one is left alone.
	before, _ := fx.nm.profile(settingPath(1))
	if _, err := w.Connect(t.Context(), ap, ""); err != nil {
		t.Fatal(err)
	}
	after, _ := fx.nm.profile(settingPath(1))
	if variantString(after["802-11-wireless-security"]["psk"]) != variantString(before["802-11-wireless-security"]["psk"]) {
		t.Error("connecting without a password rewrote the saved one")
	}
}

func TestForgetDeletesEveryProfileOfTheNetwork(t *testing.T) {
	fx, w := wifiFixture(t, withProfiles(
		wifiConnection("a", "Home", []byte("home")),
		wifiConnection("b", "Home 2", []byte("home")),
		wifiConnection("c", "Cafe", []byte("cafe")),
	))
	if known := w.KnownSSIDs(); !known["home"] || !known["cafe"] || known["other"] {
		t.Errorf("known = %v", known)
	}
	w.Forget(t.Context(), "home")
	waitFor(t, "the profiles to go", func() bool { return len(fx.svc.Settings.ForSSID([]byte("home"))) == 0 })
	if len(fx.svc.Settings.ForSSID([]byte("cafe"))) != 1 {
		t.Error("forgetting home deleted another network's profile")
	}
}

func TestTheRadioAndTheScan(t *testing.T) {
	fx, w := wifiFixture(t, nil)
	must(t, w.SetEnabled(t.Context(), false))
	if on, err := w.Enabled(t.Context()); err != nil || on {
		t.Errorf("enabled = %v, %v", on, err)
	}
	must(t, w.SetEnabled(t.Context(), true))
	if on, _ := w.Enabled(t.Context()); !on {
		t.Error("the radio did not come back on")
	}
	before := w.LastScan(t.Context())
	w.ScanAndWait(t.Context(), time.Second)
	if w.LastScan(t.Context()) == before {
		t.Error("the scan did not move LastScan")
	}
	fx.nm.wifi.addAP(fakeAP{ssid: "cafe", strength: 50})
	aps, err := w.AccessPoints(t.Context())
	if err != nil || len(aps) != 1 || aps[0].Name() != "cafe" || aps[0].Strength != 50 {
		t.Errorf("aps = %+v, %v", aps, err)
	}
	// No active connection: disconnecting is a no-op.
	must(t, w.Disconnect(t.Context()))
}

func TestConnectAndWaitReportsStepsUntilActivated(t *testing.T) {
	fx, w := wifiFixture(t, nil)
	ap := fx.nm.wifi.addAP(fakeAP{ssid: "cafe", strength: 50})
	var steps []DeviceState
	done := make(chan error, 1)
	go func() {
		done <- w.ConnectAndWait(t.Context(), ap, "", func(s DeviceState) { steps = append(steps, s) })
	}()
	waitFor(t, "the activation", func() bool { fx.nm.mu.Lock(); defer fx.nm.mu.Unlock(); return len(fx.nm.addAndActive) == 1 })
	fx.nm.wifi.deviceState(DevicePrepare, DeviceReasonNone)
	fx.nm.wifi.deviceState(DeviceDisconnected, DeviceReasonNewActivation)
	fx.nm.wifi.deviceState(DeviceIPConfig, DeviceReasonNone)
	fx.nm.wifi.deviceState(DeviceActivated, DeviceReasonNone)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the connect never finished")
	}
	if !slices.Equal(steps, []DeviceState{DevicePrepare, DeviceIPConfig}) {
		t.Errorf("steps = %v; a transient disconnect is not a step", steps)
	}
}

func TestConnectAndWaitNamesAWrongPassword(t *testing.T) {
	fx, w := wifiFixture(t, nil)
	ap := fx.nm.wifi.addAP(fakeAP{ssid: "cafe", strength: 50, rsn: secKeyMgmtPSK})
	done := make(chan error, 1)
	go func() { done <- w.ConnectAndWait(t.Context(), ap, "wrong", nil) }()
	waitFor(t, "the activation", func() bool { fx.nm.mu.Lock(); defer fx.nm.mu.Unlock(); return len(fx.nm.addAndActive) == 1 })
	fx.nm.wifi.deviceState(DeviceNeedAuth, DeviceReasonNone)
	fx.nm.wifi.deviceState(DeviceFailed, DeviceReasonNoSecrets)
	err := <-done
	if ce, ok := errors.AsType[*ConnectError](err); !ok || ce.Failure != FailureAuth {
		t.Errorf("err = %v, want an auth failure", err)
	}
}

func TestDeviceFailureVocabulary(t *testing.T) {
	for reason, want := range map[DeviceReason]ConnectFailure{
		DeviceReasonNoSecrets:           FailureAuth,
		DeviceReasonSupplicantFailed:    FailureAuth,
		DeviceReasonSupplicantTimeout:   FailureTimeout,
		DeviceReasonSSIDNotFound:        FailureNotFound,
		DeviceReasonIPConfigUnavailable: FailureIPConfig,
		DeviceReasonUserRequested:       FailureSuperseded,
		DeviceReason(99):                FailureGeneric,
	} {
		if got := classifyDeviceFailure(reason); got != want {
			t.Errorf("reason %d = %v, want %v", reason, got, want)
		}
	}
	if done, _ := deviceStep(DeviceDisconnected, DeviceReasonUnknown, nil); done {
		t.Error("a transient disconnect ended the watch")
	}
	if done, err := deviceStep(DeviceDisconnected, DeviceReasonSSIDNotFound, nil); !done || err == nil {
		t.Error("a real disconnect did not end the watch")
	}
}
