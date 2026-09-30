package network

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/service/network/openconnect"
)

// This file is what kinds of VPN this machine can run and what each
// needs to be asked for (vpn/kinds.rs). WireGuard is always available:
// NetworkManager carries it natively. Everything else needs a plugin,
// and NM advertises those as .name files, one INI per plugin naming its
// D-Bus service; reading them means the type picker offers exactly what
// can work here.

// pluginDirectories are where NM looks for plugin descriptors. A file
// in a later directory shadows the same name in an earlier one.
var pluginDirectories = []string{
	"/usr/lib/NetworkManager/VPN",
	"/usr/lib64/NetworkManager/VPN",
	"/etc/NetworkManager/VPN",
}

// WireGuard is the id of the built-in WireGuard kind: a connection
// type, not a VPN plugin.
const WireGuard = "wireguard"

// VPNFormat is what a field's value has to look like to be worth
// sending to NetworkManager. NM validates too but refuses with a raw
// D-Bus error, and the address builder silently drops what it cannot
// parse, so checking here makes the complaint name the field.
type VPNFormat int

// Field formats.
const (
	// FormatText is anything non-empty.
	FormatText VPNFormat = iota
	// FormatHost is a hostname or address, no scheme and no path.
	FormatHost
	// FormatHostPort is a host with a port: vpn.example.com:51820.
	FormatHostPort
	// FormatIPList is one or more IP addresses, comma separated.
	FormatIPList
	// FormatCIDRList is one or more addresses, each optionally with a
	// prefix length.
	FormatCIDRList
	// FormatKey is a base64 X25519 key, as WireGuard writes them.
	FormatKey
	// FormatNumber is a non-negative whole number.
	FormatNumber
)

// Accepts reports whether value is in this format. An empty value is
// not this check's business: that is what Required is for.
func (f VPNFormat) Accepts(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	switch f {
	case FormatHost:
		return isHost(value)
	case FormatHostPort:
		return isHostPort(value)
	case FormatIPList:
		return everyItem(value, func(item string) bool { _, err := netip.ParseAddr(item); return err == nil })
	case FormatCIDRList:
		return everyItem(value, isCIDR)
	case FormatKey:
		return isWireGuardKey(value)
	case FormatNumber:
		_, err := strconv.ParseUint(value, 10, 64)
		return err == nil
	}
	return true
}

// everyItem reports whether every comma-separated item passes check;
// an empty list does not.
func everyItem(raw string, check func(string) bool) bool {
	seen := false
	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !check(item) {
			return false
		}
		seen = true
	}
	return seen
}

// isHost accepts an address, or a name that could resolve to one.
// Deliberately loose: the point is to catch https://vpn.example.com/portal.
func isHost(value string) bool {
	if _, err := netip.ParseAddr(value); err == nil {
		return true
	}
	if value == "" || strings.ContainsAny(value, "/ :@") {
		return false
	}
	for part := range strings.SplitSeq(value, ".") {
		if part == "" {
			return false
		}
		for _, c := range part {
			if !isASCIIAlnum(c) && c != '-' {
				return false
			}
		}
	}
	return true
}

func isASCIIAlnum(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// isHostPort accepts host:port, with an IPv6 address bracketed:
// [fd00::1]:51820. An unbracketed v6 address is ambiguous.
func isHostPort(value string) bool {
	if rest, ok := strings.CutPrefix(value, "["); ok {
		address, port, ok := strings.Cut(rest, "]:")
		if !ok {
			return false
		}
		addr, err := netip.ParseAddr(address)
		return err == nil && addr.Is6() && isPort(port)
	}
	host, port, found := strings.CutLast(value, ":")
	if !found {
		return false
	}
	return !strings.Contains(host, ":") && isHost(host) && isPort(port)
}

func isPort(value string) bool {
	port, err := strconv.ParseUint(value, 10, 16)
	return err == nil && port != 0
}

func isCIDR(value string) bool {
	address, prefix, hasPrefix := strings.Cut(value, "/")
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	if !hasPrefix {
		return true
	}
	limit := uint64(128)
	if addr.Is4() {
		limit = 32
	}
	bits, err := strconv.ParseUint(prefix, 10, 32)
	return err == nil && bits <= limit
}

// isWireGuardKey accepts 32 bytes of base64: 43 characters and "=".
func isWireGuardKey(value string) bool {
	if len(value) != 44 || value[43] != '=' {
		return false
	}
	for _, c := range value[:43] {
		if !isASCIIAlnum(c) && c != '+' && c != '/' {
			return false
		}
	}
	return true
}

// VPNChoice is one choice of a field that is a picker.
type VPNChoice struct {
	// Value is what is stored in the profile.
	Value string
	// Label is what the picker calls it.
	Label string
	// NativeSignIn says whether wayle signs into this choice itself;
	// the form says so while the choice is made, instead of the first
	// connect failing with "no native sign-in".
	NativeSignIn bool
}

// VPNField is one field of the add/edit form.
type VPNField struct {
	// Key is where the value goes, interpreted per kind by the profile
	// builder.
	Key string
	// Label is the English label.
	Label string
	// Secret masks the input and keeps it out of the profile's data.
	Secret bool
	// Required refuses a save without it.
	Required bool
	// Placeholder is an example value.
	Placeholder string
	// Choices are the values a picker accepts; empty is free text.
	Choices []VPNChoice
	// Format is what a value has to look like.
	Format VPNFormat
	// Section groups the form, as a slug the UI turns into a heading;
	// empty stands on its own.
	Section string
}

func field(key, label, placeholder string) VPNField {
	return VPNField{Key: key, Label: label, Placeholder: placeholder}
}

func (f VPNField) required() VPNField          { f.Required = true; return f }
func (f VPNField) secret() VPNField            { f.Secret = true; return f }
func (f VPNField) format(v VPNFormat) VPNField { f.Format = v; return f }
func (f VPNField) section(s string) VPNField   { f.Section = s; return f }

func (f VPNField) choices(c []VPNChoice) VPNField { f.Choices = c; return f }

// VPNKind is a kind of VPN that can be created here.
type VPNKind struct {
	// ID is WireGuard or the plugin's D-Bus service name.
	ID string
	// Label is what the picker calls it.
	Label string
	// Fields to ask for; empty means only the free-form editor.
	Fields []VPNField
}

// IsTyped reports whether the kind has a purpose-built form.
func (k VPNKind) IsTyped() bool { return len(k.Fields) > 0 }

// AvailableKinds lists every VPN kind this machine can run, WireGuard
// first.
// the protocol free text.
func AvailableKinds() []VPNKind {
	kinds := []VPNKind{wireGuardKind()}
	for _, plugin := range installedPlugins(pluginDirectories) {
		kinds = append(kinds, VPNKind{
			ID:     plugin.service,
			Label:  displayLabel(plugin.service, plugin.name),
			Fields: fieldsForPlugin(plugin.service),
		})
	}
	return kinds
}

func wireGuardKind() VPNKind {
	return VPNKind{ID: WireGuard, Label: "WireGuard", Fields: []VPNField{
		field("interface", "Interface", "wg0").required().section("interface"),
		field("private-key", "Private key", "").required().secret().format(FormatKey).section("interface"),
		field("address", "Addresses", "10.0.0.2/24").required().format(FormatCIDRList).section("addressing"),
		field("dns", "DNS", "10.0.0.1").format(FormatIPList).section("addressing"),
		field("peer-public-key", "Peer public key", "").required().format(FormatKey).section("peer"),
		field("peer-endpoint", "Peer endpoint", "vpn.example.com:51820").required().format(FormatHostPort).section("peer"),
		field("peer-allowed-ips", "Allowed IPs", "0.0.0.0/0, ::/0").required().format(FormatCIDRList).section("peer"),
		field("peer-preshared-key", "Preshared key", "").secret().format(FormatKey).section("peer"),
		field("peer-keepalive", "Keepalive (seconds)", "25").format(FormatNumber).section("peer"),
	}}
}

// SecretKeys are the keys a plugin stores as secrets rather than plain
// data, taken from each plugin's own NM_*_KEY_* defines (checked
// against the nixpkgs sources: openvpn 1.12.5, strongswan 1.6.5,
// libreswan 1.2.31, l2tp, sstp 1.3.2, iodine, vpnc 1.4.0, fortisslvpn
// 1.4.0). Getting this wrong writes a password into vpn.data, in the
// clear. It is the single source of truth: the form marks a field
// secret by consulting it, and the profile builder decides which
// section a value lands in the same way.
func SecretKeys(service string) []string {
	switch service {
	// openconnect's secrets are all minted by a sign-in; nothing typed
	// on the form is one.
	case openconnect.ServiceType:
		return nil
	case "org.freedesktop.NetworkManager.openvpn":
		return []string{"password", "cert-pass", "http-proxy-password"}
	case "org.freedesktop.NetworkManager.vpnc":
		return []string{"IPSec secret", "Xauth password"}
	case "org.freedesktop.NetworkManager.strongswan":
		return []string{"password"}
	case "org.freedesktop.NetworkManager.libreswan":
		return []string{"xauthpassword", "pskvalue"}
	case "org.freedesktop.NetworkManager.l2tp":
		return []string{"password", "ipsec-psk", "user-certpass", "machine-certpass"}
	case "org.freedesktop.NetworkManager.pptp":
		return []string{"password"}
	case "org.freedesktop.NetworkManager.sstp":
		return []string{"password", "proxy-password", "tls-user-key-secret"}
	case "org.freedesktop.NetworkManager.fortisslvpn":
		return []string{"password", "otp"}
	case "org.freedesktop.NetworkManager.iodine":
		return []string{"password"}
	}
	return nil
}

// IsSecret reports whether key is one of service's secrets.
func IsSecret(service, key string) bool { return slices.Contains(SecretKeys(service), key) }

// fieldsForPlugin is the typed form for a known plugin, nil for one
// wayle has no form for. Every key is the plugin's own, read out of its
// source: a key the plugin does not know is silently ignored, so a form
// built on guesses would save happily and fail to connect. The keys are
// a useful subset; the tuning knobs stay reachable through the raw
// editor.
func fieldsForPlugin(service string) []VPNField {
	var fields []VPNField
	switch service {
	case openconnect.ServiceType:
		fields = []VPNField{
			field("gateway", "Gateway", "vpn.example.com").required().format(FormatHost),
			field("protocol", "Protocol", "gp").required().choices(openconnectProtocols()),
			field(openconnect.UsernameKey, "Username", "alice"),
			// Opt-in, and it has to stay that way: a gateway seeing the
			// capability may insist on a browser where it would
			// otherwise serve a form.
			field(openconnect.SSOKey, "Browser sign-in (SAML)", "").choices(choices(
				"no", "Off",
				"yes", "Sign in through the browser",
			)),
		}
	case "org.freedesktop.NetworkManager.openvpn":
		fields = []VPNField{
			field("remote", "Server", "vpn.example.com:1194").required().format(FormatHostPort).section("gateway"),
			field("connection-type", "Authentication", "").choices(choices(
				"password", "Username and password",
				"password-tls", "Username, password and certificate",
				"tls", "Certificate",
				"static-key", "Static key",
			)).section("gateway"),
			field("username", "Username", "alice").section("credentials"),
			field("password", "Password", "").section("credentials"),
			field("ca", "CA certificate", "/path/to/ca.crt").section("certificates"),
			field("cert", "User certificate", "/path/to/client.crt").section("certificates"),
			field("key", "Private key", "/path/to/client.key").section("certificates"),
			field("cert-pass", "Private key password", "").section("certificates"),
		}
	// Cisco's legacy IPsec. Its keys really do have spaces in them.
	case "org.freedesktop.NetworkManager.vpnc":
		fields = []VPNField{
			field("IPSec gateway", "Gateway", "vpn.example.com").required().format(FormatHost).section("gateway"),
			field("IPSec ID", "Group name", "employees").required().section("gateway"),
			field("IPSec secret", "Group password", "").section("gateway"),
			field("Xauth username", "Username", "alice").section("credentials"),
			field("Xauth password", "Password", "").section("credentials"),
			field("Domain", "Domain", "example.com").section("credentials"),
			field("NAT Traversal Mode", "NAT traversal", "").choices(choices(
				"natt", "NAT-T when detected",
				"cisco-udp", "Cisco UDP",
				"none", "Disabled",
			)).section("cipher"),
			field("IKE DH Group", "IKE DH group", "").choices(dhGroups(false)).section("cipher"),
			field("Perfect Forward Secrecy", "Forward secrecy", "").choices(dhGroups(true)).section("cipher"),
		}
	case "org.freedesktop.NetworkManager.strongswan":
		fields = []VPNField{
			field("address", "Gateway", "vpn.example.com").required().format(FormatHost).section("gateway"),
			field("certificate", "Gateway certificate", "/path/to/gateway.crt").section("gateway"),
			field("method", "Authentication", "").choices(choices(
				"eap", "Username and password (EAP)",
				"key", "Certificate and private key",
				"agent", "Certificate from the SSH agent",
				"smartcard", "Smartcard",
				"psk", "Pre-shared key",
			)).section("gateway"),
			field("user", "Identity", "alice@example.com").section("credentials"),
			field("password", "Password", "").section("credentials"),
			field("usercert", "User certificate", "/path/to/client.crt").section("certificates"),
			field("userkey", "Private key", "/path/to/client.key").section("certificates"),
		}
	case "org.freedesktop.NetworkManager.libreswan":
		fields = []VPNField{
			field("right", "Gateway", "vpn.example.com").required().format(FormatHost).section("gateway"),
			field("leftid", "Local identity", "alice@example.com").section("gateway"),
			field("ikev2", "IKE version", "").choices(choices(
				"insist", "IKEv2 only",
				"propose", "IKEv2 if offered",
				"no", "IKEv1 only",
			)).section("gateway"),
			field("leftxauthusername", "Username", "alice").section("credentials"),
			field("xauthpassword", "Password", "").section("credentials"),
			field("pskvalue", "Pre-shared key", "").section("credentials"),
			field("Domain", "Domain", "example.com").section("credentials"),
		}
	case "org.freedesktop.NetworkManager.l2tp":
		fields = []VPNField{
			field("gateway", "Gateway", "vpn.example.com").required().format(FormatHost).section("gateway"),
			field("user", "Username", "alice").section("credentials"),
			field("password", "Password", "").section("credentials"),
			field("domain", "Domain", "example.com").section("credentials"),
			field("ipsec-enabled", "IPsec", "").choices(yesNo()).section("ipsec"),
			field("ipsec-psk", "Pre-shared key", "").section("ipsec"),
			field("ipsec-gateway-id", "Gateway ID", "@vpn.example.com").section("ipsec"),
		}
	case "org.freedesktop.NetworkManager.pptp":
		fields = []VPNField{
			field("gateway", "Gateway", "vpn.example.com").required().format(FormatHost).section("gateway"),
			field("user", "Username", "alice").section("credentials"),
			field("password", "Password", "").section("credentials"),
			field("domain", "Domain", "example.com").section("credentials"),
			field("require-mppe", "Require encryption", "").choices(yesNo()).section("cipher"),
		}
	case "org.freedesktop.NetworkManager.sstp":
		fields = []VPNField{
			field("gateway", "Gateway", "vpn.example.com").required().format(FormatHost).section("gateway"),
			field("ca-cert", "CA certificate", "/path/to/ca.crt").section("gateway"),
			field("user", "Username", "alice").section("credentials"),
			field("password", "Password", "").section("credentials"),
			field("domain", "Domain", "example.com").section("credentials"),
		}
	case "org.freedesktop.NetworkManager.fortisslvpn":
		fields = []VPNField{
			field("gateway", "Gateway", "vpn.example.com:443").required().format(FormatHostPort).section("gateway"),
			field("trusted-cert", "Trusted certificate", "").section("gateway"),
			field("user", "Username", "alice").section("credentials"),
			field("password", "Password", "").section("credentials"),
			field("otp", "One-time code", "").section("credentials"),
			field("realm", "Realm", "").section("credentials"),
		}
	case "org.freedesktop.NetworkManager.iodine":
		fields = []VPNField{
			field("topdomain", "Top domain", "tunnel.example.com").required(),
			field("nameserver", "Nameserver", "ns.example.com").format(FormatHost),
			field("password", "Password", ""),
			field("fragsize", "Fragment size", "").format(FormatNumber),
		}
	}
	// Secrecy comes from the one list rather than from remembering to
	// mark the right rows.
	for i := range fields {
		if IsSecret(service, fields[i].Key) {
			fields[i] = fields[i].secret()
		}
	}
	return fields
}

// choices builds picker choices from value, label pairs. Everything
// here is configuration rather than a sign-in method, so each claims
// native sign-in.
func choices(pairs ...string) []VPNChoice {
	out := make([]VPNChoice, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, VPNChoice{Value: pairs[i], Label: pairs[i+1], NativeSignIn: true})
	}
	return out
}

func yesNo() []VPNChoice { return choices("yes", "Enabled", "no", "Disabled") }

// dhGroups are vpnc's Diffie-Hellman groups; forward secrecy also takes
// "leave it to the server" and "off".
func dhGroups(forwardSecrecy bool) []VPNChoice {
	var groups []VPNChoice
	if forwardSecrecy {
		groups = choices("server", "Server decides", "nopfs", "Disabled")
	}
	return append(groups, choices(
		"dh1", "Group 1",
		"dh2", "Group 2",
		"dh5", "Group 5",
		"dh14", "Group 14",
		"dh15", "Group 15",
		"dh16", "Group 16",
		"dh17", "Group 17",
		"dh18", "Group 18",
	)...)
}

// openconnectProtocols are the openconnect protocols as picker
// choices, each marked with whether wayle signs into it. A free-text
// box meant typing a protocol name from memory and finding out at
// connect time whether wayle can sign into it.
func openconnectProtocols() []VPNChoice {
	out := make([]VPNChoice, 0, len(openconnect.Protocols))
	for _, p := range openconnect.Protocols {
		out = append(out, VPNChoice{Value: p.Value, Label: p.Label, NativeSignIn: openconnect.SignsInNatively(p.Value)})
	}
	return out
}

// displayLabel names a plugin in the type picker: the ones wayle
// recognises after what they connect to, anything else in the plugin's
// own wording.
func displayLabel(service, pluginName string) string {
	switch service {
	case openconnect.ServiceType:
		return "OpenConnect (GlobalProtect, AnyConnect, Fortinet, Pulse)"
	case "org.freedesktop.NetworkManager.openvpn":
		return "OpenVPN"
	case "org.freedesktop.NetworkManager.vpnc":
		return "Cisco (vpnc)"
	case "org.freedesktop.NetworkManager.strongswan":
		return "IPsec/IKEv2 (strongSwan)"
	case "org.freedesktop.NetworkManager.libreswan":
		return "IPsec/IKEv2 (libreswan)"
	case "org.freedesktop.NetworkManager.l2tp":
		return "L2TP/IPsec"
	case "org.freedesktop.NetworkManager.pptp":
		return "PPTP"
	case "org.freedesktop.NetworkManager.sstp":
		return "SSTP"
	case "org.freedesktop.NetworkManager.fortisslvpn":
		return "Fortinet SSL VPN"
	case "org.freedesktop.NetworkManager.iodine":
		return "Iodine (DNS tunnel)"
	}
	return pluginName
}

type plugin struct{ service, name string }

// installedPlugins reads every descriptor as (service, display name),
// a later directory's entry replacing an earlier one's in place.
func installedPlugins(dirs []string) []plugin {
	var order []plugin
	for _, dir := range dirs {
		for _, p := range readPluginDirectory(dir) {
			if i := slices.IndexFunc(order, func(q plugin) bool { return q.service == p.service }); i >= 0 {
				order[i] = p
				continue
			}
			order = append(order, p)
		}
	}
	return order
}

// readPluginDirectory parses one directory's .name files, sorted by
// label so the picker does not reshuffle between runs.
func readPluginDirectory(dir string) []plugin {
	var plugins []plugin
	for _, contents := range descriptorsIn(dir) {
		if p, ok := parseNameFile(contents); ok {
			plugins = append(plugins, p)
		}
	}
	slices.SortStableFunc(plugins, func(a, b plugin) int { return strings.Compare(a.name, b.name) })
	return plugins
}

// descriptorsIn reads every .name file of a directory; a missing
// directory has none.
func descriptorsIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".name" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // a descriptor in one of NM's own plugin directories
		if err != nil {
			continue
		}
		out = append(out, string(data))
	}
	return out
}

// vpnConnectionSection walks the [VPN Connection] section's key=value
// lines of a descriptor.
func vpnConnectionSection(contents string, visit func(key, value string)) {
	inSection := false
	for line := range strings.Lines(contents) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inSection = strings.EqualFold(line, "[VPN Connection]")
			continue
		}
		if !inSection || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		visit(strings.TrimSpace(key), strings.TrimSpace(value))
	}
}

// parseNameFile pulls service and name out of a descriptor; a
// descriptor with no service is not a plugin, and one with no name
// falls back to its service.
func parseNameFile(contents string) (plugin, bool) {
	var p plugin
	vpnConnectionSection(contents, func(key, value string) {
		switch key {
		case "service":
			p.service = value
		case "name":
			p.name = value
		}
	})
	if p.service == "" {
		return plugin{}, false
	}
	if p.name == "" {
		p.name = p.service
	}
	return p, true
}

// canBePrivate reports whether NM will bring up a user-owned profile of
// this kind. WireGuard always can. A plugin VPN can only when its
// descriptor says supports-safe-private-file-access=true; NM refuses
// the activation otherwise, and NetworkManager-openconnect does not
// say it, so its profiles stay system-wide.
func canBePrivate(kind string, dirs []string) bool {
	if kind == WireGuard {
		return true
	}
	allowed := false
	for _, dir := range dirs {
		for _, contents := range descriptorsIn(dir) {
			// A later directory shadows an earlier one, as in NM.
			if verdict, ok := privateAccess(contents, kind); ok {
				allowed = verdict
			}
		}
	}
	return allowed
}

// privateAccess is what a descriptor says about private connections for
// service, ok=false when it describes another plugin. Absent means no,
// as in NM.
func privateAccess(contents, service string) (private, ok bool) {
	vpnConnectionSection(contents, func(key, value string) {
		switch key {
		case "service":
			ok = value == service
		case "supports-safe-private-file-access":
			switch strings.ToLower(value) {
			case "true", "yes", "1":
				private = true
			default:
				private = false
			}
		}
	})
	return private && ok, ok
}
