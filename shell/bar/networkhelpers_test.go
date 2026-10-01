package bar

import (
	"testing"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/network"
)

func TestSignalStrengthIcon(t *testing.T) {
	for s, want := range map[uint8]string{
		0: "cm-wireless-signal-none-symbolic", 19: "cm-wireless-signal-none-symbolic",
		20: "cm-wireless-signal-weak-symbolic", 39: "cm-wireless-signal-weak-symbolic",
		40: "cm-wireless-signal-ok-symbolic", 59: "cm-wireless-signal-ok-symbolic",
		60: "cm-wireless-signal-good-symbolic", 79: "cm-wireless-signal-good-symbolic",
		80: "cm-wireless-signal-excellent-symbolic", 100: "cm-wireless-signal-excellent-symbolic",
	} {
		if got := signalStrengthIcon(s); got != want {
			t.Errorf("signalStrengthIcon(%d) = %q, want %q", s, got, want)
		}
	}
}

func TestFrequencyBandAndWiredSpeed(t *testing.T) {
	for f, want := range map[uint32]string{
		2412: "2.4 GHz", 2484: "2.4 GHz", 5180: "5 GHz", 5825: "5 GHz",
		5955: "6 GHz", 7115: "6 GHz", 60480: "60 GHz", 0: "", 900: "",
	} {
		if got := frequencyBand(f); got != want {
			t.Errorf("frequencyBand(%d) = %q, want %q", f, got, want)
		}
	}
	for s, want := range map[uint32]string{100: "100 Mbps", 10: "10 Mbps", 1000: "1 Gbps", 2500: "2.5 Gbps", 10000: "10 Gbps"} {
		if got := formatWiredSpeed(s); got != want {
			t.Errorf("formatWiredSpeed(%d) = %q, want %q", s, got, want)
		}
	}
}

func ap(ssid string, strength uint8, sec network.SecurityType) network.AccessPoint {
	return network.AccessPoint{SSID: []byte(ssid), Strength: strength, Security: sec}
}

func ssidsOf(list []apSnapshot) []string {
	var out []string
	for _, a := range list {
		s := a.ssid
		if a.stale {
			s += "~"
		}
		out = append(out, s)
	}
	return out
}

func TestSortedUniqueAccessPoints(t *testing.T) {
	aps := []network.AccessPoint{
		ap("home", 40, network.SecurityWPA2), ap("home", 70, network.SecurityWPA2),
		ap("cafe", 90, network.SecurityNone), ap("", 99, network.SecurityNone),
		ap("corp", 80, network.SecurityEnterprise), ap("mine", 95, network.SecurityWPA2),
	}
	got := sortedUniqueAccessPoints(aps, "mine", map[string]bool{"home": true})
	if s := ssidsOf(got); len(s) != 2 || s[0] != "cafe" || s[1] != "home" {
		t.Fatalf("list = %v, want cafe, home (hidden, enterprise, connected dropped)", s)
	}
	if got[1].strength != 70 || !got[1].known || got[0].known {
		t.Errorf("dedupe/known = %+v", got)
	}
}

func TestMergeWithCache(t *testing.T) {
	snap := func(ssid string, strength uint8) apSnapshot {
		return apSnapshot{ssid: ssid, strength: strength, security: network.SecurityWPA2}
	}
	known := map[string]bool{}
	// A pruned network stays, stale.
	if s := ssidsOf(mergeWithCache([]apSnapshot{snap("home", 50)}, []apSnapshot{snap("home", 40), snap("cafe", 30)}, "", known)); len(s) != 2 || s[0] != "home" || s[1] != "cafe~" {
		t.Errorf("pruned = %v", s)
	}
	// Live wins and is not duplicated, with its live strength.
	m := mergeWithCache([]apSnapshot{snap("home", 30)}, []apSnapshot{snap("home", 90)}, "", known)
	if len(m) != 1 || m[0].stale || m[0].strength != 30 {
		t.Errorf("live = %+v", m)
	}
	// The connected network is not resurrected.
	if s := ssidsOf(mergeWithCache(nil, []apSnapshot{snap("home", 1), snap("cafe", 1)}, "home", known)); len(s) != 1 || s[0] != "cafe~" {
		t.Errorf("connected = %v", s)
	}
	// Remembered networks pick up saved-state changes.
	old := snap("cafe", 1)
	old.known = true
	m = mergeWithCache(nil, []apSnapshot{old, snap("home", 1)}, "", map[string]bool{"home": true})
	if m[0].known || !m[1].known {
		t.Errorf("known recomputed = %+v", m)
	}
	if len(mergeWithCache([]apSnapshot{snap("home", 1)}, nil, "", known)) != 1 {
		t.Error("empty cache changed the live list")
	}
}

func TestRowSecurityLabel(t *testing.T) {
	wpa := securityLabel(network.SecurityWPA2)
	if got := rowSecurityLabel(apSnapshot{security: network.SecurityWPA2}); got != wpa {
		t.Errorf("plain = %q", got)
	}
	if got := rowSecurityLabel(apSnapshot{security: network.SecurityWPA2, known: true}); got != i18n.T("dropdown-network-security-saved", i18n.Str("security", wpa)) {
		t.Errorf("saved = %q", got)
	}
	open := securityLabel(network.SecurityNone)
	if got := rowSecurityLabel(apSnapshot{security: network.SecurityNone, known: true}); got != open {
		t.Errorf("a known open network = %q, want no saved suffix", got)
	}
	if got := rowSecurityLabel(apSnapshot{security: network.SecurityWPA2, known: true, stale: true}); got != i18n.T("dropdown-network-security-stale", i18n.Str("security", wpa)) {
		t.Errorf("stale = %q", got)
	}
}

func TestConnectionStepAndFailure(t *testing.T) {
	if connectionStep(network.DevicePrepare) != i18n.T("dropdown-network-step-preparing") ||
		connectionStep(network.DeviceSecondaries) != i18n.T("dropdown-network-step-verifying") ||
		connectionStep(network.DeviceActivated) != "" {
		t.Error("connection steps")
	}
	for f, id := range map[network.ConnectFailure]string{
		network.FailureAuth: "dropdown-network-error-wrong-password", network.FailureTimeout: "dropdown-network-error-timeout",
		network.FailureNotFound: "dropdown-network-error-not-found", network.FailureIPConfig: "dropdown-network-error-ip-config",
		network.FailureGeneric: "dropdown-network-error-generic",
	} {
		if got := connectFailureMessage(f); got != i18n.T(id) {
			t.Errorf("failure %d = %q", f, got)
		}
	}
}
