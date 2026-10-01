package bar

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/network"
)

// apSnapshot is AccessPointSnapshot: one listed network.
type apSnapshot struct {
	ssid     string
	strength uint8
	security network.SecurityType
	path     dbus.ObjectPath
	known    bool
	// stale is a network an earlier scan saw that NetworkManager has
	// since pruned: its path is dead, so connecting scans for it first.
	stale bool
}

// signalStrengthIcon is signal_strength_icon.
func signalStrengthIcon(strength uint8) string {
	switch {
	case strength < 20:
		return "cm-wireless-signal-none-symbolic"
	case strength < 40:
		return "cm-wireless-signal-weak-symbolic"
	case strength < 60:
		return "cm-wireless-signal-ok-symbolic"
	case strength < 80:
		return "cm-wireless-signal-good-symbolic"
	}
	return "cm-wireless-signal-excellent-symbolic"
}

// frequencyBand is frequency_to_band; "" when unknown.
func frequencyBand(mhz uint32) string {
	switch {
	case mhz >= 2400 && mhz <= 2500:
		return "2.4 GHz"
	case mhz >= 5000 && mhz <= 5900:
		return "5 GHz"
	case mhz >= 5901 && mhz <= 7125:
		return "6 GHz"
	case mhz >= 57000 && mhz <= 71000:
		return "60 GHz"
	}
	return ""
}

// formatWiredSpeed is format_wired_speed.
func formatWiredSpeed(mbps uint32) string {
	switch {
	case mbps >= 1000 && mbps%1000 == 0:
		return strconv.FormatUint(uint64(mbps/1000), 10) + " Gbps"
	case mbps >= 1000:
		return strconv.FormatFloat(float64(mbps)/1000, 'f', 1, 64) + " Gbps"
	}
	return strconv.FormatUint(uint64(mbps), 10) + " Mbps"
}

// sortedUniqueAccessPoints is sorted_unique_access_points: one entry
// per SSID (the strongest), hidden and enterprise networks and the
// connected SSID dropped, strongest first.
func sortedUniqueAccessPoints(aps []network.AccessPoint, connected string, known map[string]bool) []apSnapshot {
	best := map[string]apSnapshot{}
	for _, ap := range aps {
		if len(ap.SSID) == 0 || ap.Security == network.SecurityEnterprise {
			continue
		}
		ssid := ap.Name()
		if ssid == connected && connected != "" {
			continue
		}
		if cur, ok := best[ssid]; ok && ap.Strength <= cur.strength {
			continue
		}
		best[ssid] = apSnapshot{ssid: ssid, strength: ap.Strength, security: ap.Security, path: ap.Path, known: known[ssid]}
	}
	out := make([]apSnapshot, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	slices.SortStableFunc(out, func(a, b apSnapshot) int {
		if c := cmp.Compare(b.strength, a.strength); c != 0 {
			return c
		}
		return cmp.Compare(a.ssid, b.ssid)
	})
	return out
}

// mergeWithCache is merge_with_cache: the live list first, then the
// cached networks it lacks, marked stale, connected dropped, known
// recomputed.
func mergeWithCache(live, cached []apSnapshot, connected string, known map[string]bool) []apSnapshot {
	liveSSIDs := map[string]bool{}
	for _, ap := range live {
		liveSSIDs[ap.ssid] = true
	}
	out := slices.Clone(live)
	for _, ap := range cached {
		if liveSSIDs[ap.ssid] || (connected != "" && ap.ssid == connected) {
			continue
		}
		ap.known = known[ap.ssid]
		ap.stale = true
		out = append(out, ap)
	}
	return out
}

// securityLabel is translate_security_type.
func securityLabel(s network.SecurityType) string {
	switch s {
	case network.SecurityWEP:
		return i18n.T("dropdown-network-security-wep")
	case network.SecurityWPA:
		return i18n.T("dropdown-network-security-wpa")
	case network.SecurityWPA2:
		return i18n.T("dropdown-network-security-wpa2")
	case network.SecurityWPA3:
		return i18n.T("dropdown-network-security-wpa3")
	case network.SecurityEnterprise:
		return i18n.T("dropdown-network-security-enterprise")
	}
	return i18n.T("dropdown-network-security-open")
}

// rowSecurityLabel is the row's second line (network_item init_model):
// stale beats saved, saved only for a secured network.
func rowSecurityLabel(ap apSnapshot) string {
	base := securityLabel(ap.security)
	switch {
	case ap.stale:
		return i18n.T("dropdown-network-security-stale", i18n.Str("security", base))
	case ap.known && ap.security.RequiresPassword():
		return i18n.T("dropdown-network-security-saved", i18n.Str("security", base))
	}
	return base
}

// connectionStep is translate_connection_step; "" for states without
// a step to show.
func connectionStep(state network.DeviceState) string {
	switch state {
	case network.DevicePrepare:
		return i18n.T("dropdown-network-step-preparing")
	case network.DeviceConfig:
		return i18n.T("dropdown-network-step-configuring")
	case network.DeviceNeedAuth:
		return i18n.T("dropdown-network-step-authenticating")
	case network.DeviceIPConfig:
		return i18n.T("dropdown-network-step-obtaining-ip")
	case network.DeviceIPCheck, network.DeviceSecondaries:
		return i18n.T("dropdown-network-step-verifying")
	}
	return ""
}

// connectFailureMessage is translate_device_failure's text; auth and
// superseded failures are handled before it (re-prompt, nothing).
func connectFailureMessage(f network.ConnectFailure) string {
	switch f {
	case network.FailureAuth:
		return i18n.T("dropdown-network-error-wrong-password")
	case network.FailureTimeout:
		return i18n.T("dropdown-network-error-timeout")
	case network.FailureNotFound:
		return i18n.T("dropdown-network-error-not-found")
	case network.FailureIPConfig:
		return i18n.T("dropdown-network-error-ip-config")
	}
	return i18n.T("dropdown-network-error-generic")
}
