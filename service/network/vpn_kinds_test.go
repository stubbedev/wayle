package network

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/wayle/service/network/openconnect"
)

const openconnectDescriptor = "[VPN Connection]\n" +
	"name=openconnect\n" +
	"service=org.freedesktop.NetworkManager.openconnect\n" +
	"program=/usr/libexec/nm-openconnect-service\n" +
	"\n" +
	"[GNOME]\n" +
	"auth-dialog=/usr/libexec/nm-openconnect-auth-dialog\n"

func TestAPluginDescriptorYieldsItsServiceAndName(t *testing.T) {
	p, ok := parseNameFile(openconnectDescriptor)
	if !ok || p.service != openconnect.ServiceType || p.name != "openconnect" {
		t.Errorf("parse = %+v %v", p, ok)
	}
	// A service= under another section is not a plugin.
	if _, ok := parseNameFile("[GNOME]\nservice=org.example.NotAVpn\n"); ok {
		t.Error("read a key outside [VPN Connection]")
	}
	if _, ok := parseNameFile("[VPN Connection]\nname=broken\n"); ok {
		t.Error("a descriptor with no service is not a plugin")
	}
	if p, _ := parseNameFile("[VPN Connection]\nservice=org.example.Vpn\n"); p.name != "org.example.Vpn" {
		t.Errorf("a nameless plugin is called %q, want its service", p.name)
	}
}

func TestPrivateAccessIsWhatTheDescriptorSays(t *testing.T) {
	if private, ok := privateAccess(openconnectDescriptor, openconnect.ServiceType); !ok || private {
		t.Errorf("openconnect as shipped: private=%v ok=%v, want false true", private, ok)
	}
	declared := "[VPN Connection]\nservice=org.freedesktop.NetworkManager.openvpn\nsupports-safe-private-file-access=true\n"
	if private, ok := privateAccess(declared, "org.freedesktop.NetworkManager.openvpn"); !ok || !private {
		t.Error("a plugin that says so can take a private profile")
	}
	if _, ok := privateAccess(declared, openconnect.ServiceType); ok {
		t.Error("another plugin's descriptor spoke for this one")
	}
	elsewhere := "[GNOME]\nsupports-safe-private-file-access=true\n[VPN Connection]\nservice=org.example.Vpn\n"
	if private, _ := privateAccess(elsewhere, "org.example.Vpn"); private {
		t.Error("the key outside the section was read")
	}
}

func TestCanBePrivateReadsTheInstalledDescriptors(t *testing.T) {
	early, late := t.TempDir(), t.TempDir()
	write := func(dir, name, contents string) {
		must(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600))
	}
	write(early, "openvpn.name", "[VPN Connection]\nservice=org.freedesktop.NetworkManager.openvpn\nsupports-safe-private-file-access=true\n")
	write(early, "openconnect.name", openconnectDescriptor)
	dirs := []string{early, late}
	if !canBePrivate(WireGuard, nil) {
		t.Error("WireGuard needs no plugin")
	}
	if !canBePrivate("org.freedesktop.NetworkManager.openvpn", dirs) {
		t.Error("openvpn declared private access")
	}
	if canBePrivate(openconnect.ServiceType, dirs) {
		t.Error("openconnect does not declare private access")
	}
	if canBePrivate("org.example.Missing", dirs) {
		t.Error("a plugin that is not installed")
	}
	// A later directory shadows an earlier one, as in NM.
	write(late, "openvpn.name", "[VPN Connection]\nservice=org.freedesktop.NetworkManager.openvpn\n")
	if canBePrivate("org.freedesktop.NetworkManager.openvpn", dirs) {
		t.Error("the /etc override did not shadow the packaged descriptor")
	}
}

func TestInstalledPluginsAreOfferedOncePerServiceInStableOrder(t *testing.T) {
	early, late := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(early, "b.name"), []byte("[VPN Connection]\nservice=org.b\nname=Bravo\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(early, "a.name"), []byte("[VPN Connection]\nservice=org.a\nname=Alpha\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(early, "notes.txt"), []byte("[VPN Connection]\nservice=org.c\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(late, "b.name"), []byte("[VPN Connection]\nservice=org.b\nname=Bravo (local)\n"), 0o600))
	got := installedPlugins([]string{early, "/nonexistent", late})
	want := []plugin{{"org.a", "Alpha"}, {"org.b", "Bravo (local)"}}
	if !slices.Equal(got, want) {
		t.Errorf("plugins = %+v, want %+v", got, want)
	}
}

func TestWireGuardIsOfferedWithoutAnyPluginAndGrouped(t *testing.T) {
	kinds := AvailableKinds()
	if kinds[0].ID != WireGuard || !kinds[0].IsTyped() {
		t.Fatalf("first kind = %+v", kinds[0])
	}
	var sections []string
	for _, f := range wireGuardKind().Fields {
		sections = append(sections, f.Section)
	}
	want := []string{"interface", "interface", "addressing", "addressing", "peer", "peer", "peer", "peer", "peer"}
	if !slices.Equal(sections, want) {
		t.Errorf("sections = %v; each group must be contiguous", sections)
	}
	format := func(key string) VPNFormat {
		for _, f := range wireGuardKind().Fields {
			if f.Key == key {
				return f.Format
			}
		}
		return -1
	}
	for key, want := range map[string]VPNFormat{
		"private-key": FormatKey, "address": FormatCIDRList, "dns": FormatIPList,
		"peer-endpoint": FormatHostPort, "peer-keepalive": FormatNumber, "interface": FormatText,
	} {
		if got := format(key); got != want {
			t.Errorf("%s format = %v, want %v", key, got, want)
		}
	}
}

func TestFormats(t *testing.T) {
	for _, f := range []VPNFormat{FormatHost, FormatHostPort, FormatIPList, FormatCIDRList, FormatKey, FormatNumber} {
		if !f.Accepts("") || !f.Accepts("   ") {
			t.Errorf("format %v rejected an empty value; that is Required's business", f)
		}
	}
	accept := map[VPNFormat][]string{
		FormatCIDRList: {"10.0.0.2/24", "10.0.0.2", "0.0.0.0/0, ::/0", "fd00::2/64"},
		FormatIPList:   {"10.0.0.1, fd00::1"},
		FormatHostPort: {"vpn.example.com:51820", "10.0.0.1:51820", "[fd00::1]:51820"},
		FormatHost:     {"vpn.example.com", "10.0.0.1", "fd00::1"},
		FormatKey:      {"6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk="},
		FormatNumber:   {"25"},
		FormatText:     {"wg0", "anything at all / really"},
	}
	reject := map[VPNFormat][]string{
		FormatCIDRList: {"10.0.0.256/24", "10.0.0.2/33", "fd00::2/129", "10.0.0.2, nonsense", ","},
		FormatIPList:   {"10.0.0.1/24", "dns.example.com"},
		FormatHostPort: {"vpn.example.com", "vpn.example.com:0", "vpn.example.com:99999", "fd00::1:51820"},
		FormatHost:     {"https://vpn.example.com", "vpn.example.com/portal", "vpn example com"},
		FormatKey:      {"6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk", "not-a-key", "6HeTLQTdIcJHFmwCNBjMFR nGiEBDSQMCsBcgWJZ7Fk="},
		FormatNumber:   {"25s", "-1"},
	}
	for format, values := range accept {
		for _, v := range values {
			if !format.Accepts(v) {
				t.Errorf("format %v rejected %q", format, v)
			}
		}
	}
	for format, values := range reject {
		for _, v := range values {
			if format.Accepts(v) {
				t.Errorf("format %v accepted %q", format, v)
			}
		}
	}
}

func TestTheProtocolFieldIsAPickerOverWhatOpenconnectSpeaks(t *testing.T) {
	fields := fieldsForPlugin(openconnect.ServiceType)
	var protocol VPNField
	for _, f := range fields {
		if f.Key == "protocol" {
			protocol = f
		}
		if f.Key == "gateway" && len(f.Choices) > 0 {
			t.Error("a free-text field offers choices")
		}
		if f.Section != "" {
			t.Errorf("openconnect field %s is grouped; it has nothing to group", f.Key)
		}
	}
	var values []string
	for _, c := range protocol.Choices {
		values = append(values, c.Value)
		if c.NativeSignIn != openconnect.SignsInNatively(c.Value) {
			t.Errorf("%s: the choice disagrees with the sign-in's own verdict", c.Value)
		}
	}
	// The order the picker offers, the Rust PROTOCOLS order.
	if !slices.Equal(values, []string{"gp", "anyconnect", "fortinet", "array", "nc", "pulse", "f5"}) {
		t.Errorf("protocols = %v", values)
	}
}

func TestPluginLabels(t *testing.T) {
	if got := displayLabel(openconnect.ServiceType, "openconnect"); got != "OpenConnect (GlobalProtect, AnyConnect, Fortinet, Pulse)" {
		t.Errorf("openconnect label = %q", got)
	}
	if got := displayLabel("org.freedesktop.NetworkManager.openvpn", "openvpn"); got != "OpenVPN" {
		t.Errorf("openvpn label = %q", got)
	}
	if got := displayLabel("org.example.SomeVpn", "Some VPN"); got != "Some VPN" {
		t.Errorf("an unrecognised plugin must keep its own wording, got %q", got)
	}
}

var typedPlugins = []string{
	openconnect.ServiceType,
	"org.freedesktop.NetworkManager.openvpn",
	"org.freedesktop.NetworkManager.vpnc",
	"org.freedesktop.NetworkManager.strongswan",
	"org.freedesktop.NetworkManager.libreswan",
	"org.freedesktop.NetworkManager.l2tp",
	"org.freedesktop.NetworkManager.pptp",
	"org.freedesktop.NetworkManager.sstp",
	"org.freedesktop.NetworkManager.fortisslvpn",
	"org.freedesktop.NetworkManager.iodine",
}

func TestEveryTypedPluginAsksForItsGatewayFirstAndStaysShort(t *testing.T) {
	for _, service := range typedPlugins {
		fields := fieldsForPlugin(service)
		if len(fields) == 0 {
			t.Fatalf("%s has no typed form", service)
		}
		if !fields[0].Required {
			t.Errorf("%s's first field %s is not required", service, fields[0].Key)
		}
		if len(fields) > 12 {
			t.Errorf("%s asks for %d fields; that belongs in the raw editor", service, len(fields))
		}
	}
	if fieldsForPlugin("org.example.SomeVpnPlugin") != nil || SecretKeys("org.example.SomeVpnPlugin") != nil {
		t.Error("a plugin wayle has never seen got a typed form or secrets")
	}
}

func TestAPasswordFieldIsMarkedSecretFromTheOneList(t *testing.T) {
	for _, service := range typedPlugins {
		for _, f := range fieldsForPlugin(service) {
			if f.Secret != IsSecret(service, f.Key) {
				t.Errorf("%s: %s disagrees with the secret list", service, f.Key)
			}
		}
	}
}

func TestEverySecretKeyIsTheRealKeyOfItsPlugin(t *testing.T) {
	for service, want := range map[string][]string{
		"org.freedesktop.NetworkManager.vpnc":        {"IPSec secret", "Xauth password"},
		"org.freedesktop.NetworkManager.libreswan":   {"xauthpassword", "pskvalue"},
		"org.freedesktop.NetworkManager.fortisslvpn": {"password", "otp"},
		"org.freedesktop.NetworkManager.openvpn":     {"password", "cert-pass", "http-proxy-password"},
		"org.freedesktop.NetworkManager.sstp":        {"password", "proxy-password", "tls-user-key-secret"},
		"org.freedesktop.NetworkManager.l2tp":        {"password", "ipsec-psk", "user-certpass", "machine-certpass"},
	} {
		if got := SecretKeys(service); !slices.Equal(got, want) {
			t.Errorf("%s secrets = %q, want %q", service, got, want)
		}
	}
	// vpnc's keys really do contain spaces; a tidied key is another key.
	if !IsSecret("org.freedesktop.NetworkManager.vpnc", "IPSec secret") || IsSecret("org.freedesktop.NetworkManager.vpnc", "ipsec-secret") {
		t.Error("vpnc's spaced key")
	}
	// libreswan's Domain is capitalised and is not a secret.
	found := false
	for _, f := range fieldsForPlugin("org.freedesktop.NetworkManager.libreswan") {
		if f.Key == "Domain" {
			found = !f.Secret
		}
	}
	if !found {
		t.Error("libreswan's Domain key")
	}
}

func TestOpenconnectKeepsNothingTypedAsASecret(t *testing.T) {
	if SecretKeys(openconnect.ServiceType) != nil {
		t.Error("openconnect has typed secrets")
	}
	for _, f := range fieldsForPlugin(openconnect.ServiceType) {
		if f.Secret {
			t.Errorf("openconnect field %s is secret", f.Key)
		}
	}
}

func TestAPickerOffersOnlyValuesItsPluginAccepts(t *testing.T) {
	find := func(key string) VPNField {
		for _, f := range fieldsForPlugin("org.freedesktop.NetworkManager.vpnc") {
			if f.Key == key {
				return f
			}
		}
		t.Fatalf("vpnc has no %s", key)
		return VPNField{}
	}
	var natt []string
	for _, c := range find("NAT Traversal Mode").Choices {
		natt = append(natt, c.Value)
	}
	if !slices.Equal(natt, []string{"natt", "cisco-udp", "none"}) {
		t.Errorf("natt = %v", natt)
	}
	pfs, ike := find("Perfect Forward Secrecy").Choices, find("IKE DH Group").Choices
	if len(pfs) != len(ike)+2 || pfs[0].Value != "server" || ike[0].Value != "dh1" {
		t.Errorf("pfs = %v, ike = %v", pfs, ike)
	}
}
