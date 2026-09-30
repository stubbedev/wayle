package network

import (
	"net/netip"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/network/openconnect"
)

// This file turns form values into a NetworkManager connection profile,
// and back (vpn/profile.rs). That is the whole of configuring a VPN:
// AddConnection takes a nested dictionary, and every VPN, plugin or
// native WireGuard, is that same call with a different middle section.

// notSaved is NM_SETTING_SECRET_FLAG_NOT_SAVED: never store it, always
// ask the agent. Right for everything openconnect needs, all of which a
// sign-in mints fresh and which expires.
const notSaved = "2"

// openconnectEphemeral are the openconnect secrets that must come from
// an agent every time: exactly the three keys the plugin's
// need_secrets looks for.
var openconnectEphemeral = []string{"cookie", "gateway", "gwcert"}

// BuildProfile builds the profile for a kind from the form's values.
// uuid is supplied rather than generated so an edit rewrites the
// profile it is editing instead of creating a second one.
func BuildProfile(kind, name, uuid string, values map[string]string) ConnectionDict {
	if kind == WireGuard {
		return wireGuardProfile(name, uuid, values)
	}
	return pluginProfile(kind, name, uuid, values)
}

func connectionSettings(name, uuid, connectionType, iface string) map[string]dbus.Variant {
	section := map[string]dbus.Variant{
		"id":   dbus.MakeVariant(name),
		"uuid": dbus.MakeVariant(uuid),
		"type": dbus.MakeVariant(connectionType),
		// Never autoconnect: a VPN dialling itself on every boot is a
		// surprise, and one needing 2FA would raise a prompt nobody
		// asked for.
		"autoconnect": dbus.MakeVariant(false),
	}
	if iface != "" {
		section["interface-name"] = dbus.MakeVariant(iface)
	}
	return section
}

func ipMethod(method string) map[string]dbus.Variant {
	return map[string]dbus.Variant{"method": dbus.MakeVariant(method)}
}

// pluginProfile is a plugin VPN: everything but the vpn section is
// boilerplate.
func pluginProfile(service, name, uuid string, values map[string]string) ConnectionDict {
	data := map[string]string{}
	secretValues := map[string]string{}
	for key, value := range values {
		if value == "" {
			continue
		}
		if IsSecret(service, key) {
			secretValues[key] = value
			data[key+"-flags"] = "0"
		} else {
			data[key] = value
		}
	}
	if service == openconnect.ServiceType {
		for _, key := range openconnectEphemeral {
			data[key+"-flags"] = notSaved
		}
		// The plugin reads this to decide whether to run the
		// Windows-only host-check trojan. It will not, and saying so
		// avoids a stall.
		data["enable_csd_trojan"] = "no"
	}
	if service == "org.freedesktop.NetworkManager.openvpn" && len(values) > 0 {
		if _, set := data["connection-type"]; !set {
			data["connection-type"] = "password"
		}
	}

	vpn := map[string]dbus.Variant{
		"service-type": dbus.MakeVariant(service),
		"data":         dbus.MakeVariant(data),
	}
	if len(secretValues) > 0 {
		vpn["secrets"] = dbus.MakeVariant(secretValues)
	}
	// openconnect can carry a tunnel across a change of network on the
	// session it has, and its plugin tells NM so, but NM only lets it
	// when the profile asks. Without this NM tears the tunnel down with
	// the device it rode on, on a new wifi network and on every suspend,
	// and the plugin's SIGINT logs the session off.
	if service == openconnect.ServiceType {
		vpn["persistent"] = dbus.MakeVariant(true)
	}
	return ConnectionDict{
		"connection": connectionSettings(name, uuid, "vpn", ""),
		"vpn":        vpn,
		"ipv4":       ipMethod("auto"),
		"ipv6":       ipMethod("auto"),
	}
}

// wireGuardProfile is a native WireGuard profile: no plugin, the kernel
// carries it.
func wireGuardProfile(name, uuid string, values map[string]string) ConnectionDict {
	tunnel := map[string]dbus.Variant{"private-key": dbus.MakeVariant(values["private-key"])}
	if peer, ok := wireGuardPeer(values); ok {
		tunnel["peers"] = dbus.MakeVariant([]map[string]dbus.Variant{peer})
	}
	v4, v6 := splitAddresses(values["address"])
	dns4, dns6 := splitServers(values["dns"])
	return ConnectionDict{
		"connection": connectionSettings(name, uuid, WireGuard, values["interface"]),
		"wireguard":  tunnel,
		"ipv4":       ipConfig(v4, dns4),
		"ipv6":       ipConfig(v6, dns6),
	}
}

// wireGuardPeer is the single peer the form collects; multi-peer
// configurations are the free-form editor's problem.
func wireGuardPeer(values map[string]string) (map[string]dbus.Variant, bool) {
	publicKey := values["peer-public-key"]
	if publicKey == "" {
		return nil, false
	}
	peer := map[string]dbus.Variant{"public-key": dbus.MakeVariant(publicKey)}
	if endpoint := values["peer-endpoint"]; endpoint != "" {
		peer["endpoint"] = dbus.MakeVariant(endpoint)
	}
	if preshared := values["peer-preshared-key"]; preshared != "" {
		peer["preshared-key"] = dbus.MakeVariant(preshared)
		// Without this NM treats the preshared key as absent.
		peer["preshared-key-flags"] = dbus.MakeVariant(uint32(0))
	}
	if allowed := splitList(values["peer-allowed-ips"]); len(allowed) > 0 {
		peer["allowed-ips"] = dbus.MakeVariant(allowed)
	}
	if keepalive, err := strconv.ParseUint(strings.TrimSpace(values["peer-keepalive"]), 10, 32); err == nil {
		peer["persistent-keepalive"] = dbus.MakeVariant(uint32(keepalive))
	}
	return peer, true
}

// address is one address with its prefix length.
type address struct {
	addr   string
	prefix uint32
}

// ipConfig is an IP section carrying explicit addresses, or disabled
// when the tunnel has none of that family: there is no DHCP inside a
// WireGuard tunnel, so auto would wait for a lease that never comes.
func ipConfig(addresses []address, dns []string) map[string]dbus.Variant {
	if len(addresses) == 0 {
		return ipMethod("disabled")
	}
	entries := make([]map[string]dbus.Variant, 0, len(addresses))
	for _, a := range addresses {
		entries = append(entries, map[string]dbus.Variant{
			"address": dbus.MakeVariant(a.addr),
			"prefix":  dbus.MakeVariant(a.prefix),
		})
	}
	section := ipMethod("manual")
	section["address-data"] = dbus.MakeVariant(entries)
	if len(dns) > 0 {
		// dns-data, not dns: the latter is network-byte-order integers,
		// a portability trap for no gain.
		section["dns-data"] = dbus.MakeVariant(dns)
	}
	return section
}

// splitList splits a comma- or whitespace-separated list, dropping
// empties.
func splitList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
}

// splitAddresses splits "10.0.0.2/24, fd00::2/64" by family, defaulting
// the prefix to a single host and clamping it to the family; what is
// not an address is dropped rather than sent.
func splitAddresses(raw string) (v4, v6 []address) {
	for _, entry := range splitList(raw) {
		addrText, prefixText, hasPrefix := strings.Cut(entry, "/")
		addr, err := netip.ParseAddr(addrText)
		if err != nil {
			continue
		}
		limit := uint32(128)
		if addr.Is4() {
			limit = 32
		}
		prefix := limit
		if hasPrefix {
			if p, err := strconv.ParseUint(prefixText, 10, 32); err == nil {
				prefix = min(uint32(p), limit)
			}
		}
		if addr.Is4() {
			v4 = append(v4, address{addr: addrText, prefix: prefix})
		} else {
			v6 = append(v6, address{addr: addrText, prefix: prefix})
		}
	}
	return v4, v6
}

// splitServers splits DNS servers by family, each for its own section.
func splitServers(raw string) (v4, v6 []string) {
	for _, entry := range splitList(raw) {
		addr, err := netip.ParseAddr(entry)
		switch {
		case err != nil:
		case addr.Is4():
			v4 = append(v4, entry)
		default:
			v6 = append(v6, entry)
		}
	}
	return v4, v6
}

// ReadProfileValues reads a saved profile back into form values, for
// the edit form. The -flags keys are wayle's own bookkeeping and stay
// off the form.
func ReadProfileValues(dict ConnectionDict) map[string]string {
	values := map[string]string{}
	if data, ok := dict["vpn"]["data"].Value().(map[string]string); ok {
		for key, value := range data {
			if !strings.HasSuffix(key, "-flags") {
				values[key] = value
			}
		}
		return values
	}
	if iface := dictReader(dict).str("connection", "interface-name"); iface != "" {
		values["interface"] = iface
	}
	return values
}

// KindOf is the kind a saved profile belongs to, "" when it is no VPN.
func KindOf(dict ConnectionDict) string {
	r := dictReader(dict)
	switch r.str("connection", "type") {
	case WireGuard:
		return WireGuard
	case "":
		return ""
	}
	return r.str("vpn", "service-type")
}

// ownedBy makes a profile its user's own: connection.permissions names
// them and nobody else. For a profile that names only its caller NM
// checks settings.modify.own, which it grants the active local user,
// where a system-wide profile needs settings.modify.system, an
// administrator's password on a stock install. NM's own form, reserved
// field and all: "user:NAME:" is what it stores and reads back.
func ownedBy(dict ConnectionDict, user string) {
	if section, ok := dict["connection"]; ok {
		section["permissions"] = dbus.MakeVariant([]string{"user:" + user + ":"})
	}
}
