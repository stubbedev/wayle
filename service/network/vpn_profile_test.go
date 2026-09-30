package network

import (
	"encoding/base64"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/network/openconnect"
)

func dataOf(t *testing.T, dict ConnectionDict) map[string]string {
	t.Helper()
	data, ok := dict["vpn"]["data"].Value().(map[string]string)
	if !ok {
		t.Fatalf("vpn.data = %v", dict["vpn"]["data"])
	}
	return data
}

func TestAWireGuardProfileNeedsNoPluginSection(t *testing.T) {
	p := BuildProfile(WireGuard, "Home", "uuid-1", map[string]string{
		"interface":        "wg0",
		"private-key":      "PRIVATE",
		"address":          "10.0.0.2/24",
		"peer-public-key":  "PUBLIC",
		"peer-endpoint":    "vpn.example.com:51820",
		"peer-allowed-ips": "0.0.0.0/0",
	})
	r := dictReader(p)
	if r.str("connection", "type") != WireGuard || r.str("connection", "interface-name") != "wg0" {
		t.Errorf("connection = %v", p["connection"])
	}
	if _, ok := p["wireguard"]; !ok {
		t.Error("no wireguard section")
	}
	if _, ok := p["vpn"]; ok {
		t.Error("a WireGuard profile with a vpn section is two connection types at once")
	}
	peers, _ := p["wireguard"]["peers"].Value().([]map[string]dbus.Variant)
	if len(peers) != 1 || variantString(peers[0]["public-key"]) != "PUBLIC" {
		t.Errorf("peers = %v", p["wireguard"]["peers"])
	}
}

func TestAWireGuardTunnelGetsManualAddressingNotDHCP(t *testing.T) {
	p := BuildProfile(WireGuard, "Home", "uuid-1", map[string]string{"address": "10.0.0.2/24", "peer-public-key": "PUBLIC", "dns": "10.0.0.1"})
	if got := dictReader(p).str("ipv4", "method"); got != "manual" {
		t.Errorf("ipv4 method = %q", got)
	}
	if got := dictReader(p).str("ipv6", "method"); got != "disabled" {
		t.Errorf("ipv6 method = %q; auto would hang on a lease", got)
	}
	if dns, _ := p["ipv4"]["dns-data"].Value().([]string); !slices.Equal(dns, []string{"10.0.0.1"}) {
		t.Errorf("dns-data = %v", p["ipv4"]["dns-data"])
	}
	if _, ok := p["ipv4"]["dns"]; ok {
		t.Error("dns (network-byte-order ints) written instead of dns-data")
	}
}

func TestAPeerCarriesOnlyWhatWasGiven(t *testing.T) {
	peer, ok := wireGuardPeer(map[string]string{
		"peer-public-key": "PUBLIC", "peer-preshared-key": "PSK", "peer-keepalive": " 25 ", "peer-allowed-ips": "10.0.0.0/24, ::/0",
	})
	if !ok {
		t.Fatal("no peer")
	}
	if peer["preshared-key-flags"].Value() != uint32(0) || peer["persistent-keepalive"].Value() != uint32(25) {
		t.Errorf("peer = %v", peer)
	}
	if allowed, _ := peer["allowed-ips"].Value().([]string); !slices.Equal(allowed, []string{"10.0.0.0/24", "::/0"}) {
		t.Errorf("allowed-ips = %v", peer["allowed-ips"])
	}
	if _, ok := peer["endpoint"]; ok {
		t.Error("an empty endpoint was written")
	}
	if _, ok := wireGuardPeer(map[string]string{"peer-endpoint": "x:1"}); ok {
		t.Error("a peer without a public key")
	}
	if peer, _ := wireGuardPeer(map[string]string{"peer-public-key": "P", "peer-keepalive": "soon"}); peer["persistent-keepalive"].Value() != nil {
		t.Error("an unparsable keepalive was sent")
	}
}

func TestAnOpenconnectProfileMarksItsSecretsAsNeverStored(t *testing.T) {
	p := BuildProfile(openconnect.ServiceType, "Work", "uuid-1", map[string]string{"gateway": "vpn.example.com", "protocol": "gp"})
	data := dataOf(t, p)
	if data["gateway"] != "vpn.example.com" {
		t.Errorf("gateway = %q", data["gateway"])
	}
	var flags []string
	for key, value := range data {
		if strings.HasSuffix(key, "-flags") {
			flags = append(flags, key)
			if value != notSaved {
				t.Errorf("%s = %q, want not-saved", key, value)
			}
		}
	}
	slices.Sort(flags)
	if !slices.Equal(flags, []string{"cookie-flags", "gateway-flags", "gwcert-flags"}) {
		t.Errorf("flags = %v; only the keys the plugin asks for", flags)
	}
	if data["enable_csd_trojan"] != "no" {
		t.Error("the CSD trojan was not turned off")
	}
	if _, ok := p["vpn"]["secrets"]; ok {
		t.Error("an openconnect profile stored secrets")
	}
	if p["vpn"]["persistent"].Value() != true {
		t.Error("an openconnect tunnel must ride out a change of network")
	}
}

func TestAPluginThatNeverOfferedToPersistIsNotAsked(t *testing.T) {
	p := BuildProfile("org.freedesktop.NetworkManager.openvpn", "Office", "uuid-1", map[string]string{"remote": "vpn.example.com"})
	if _, ok := p["vpn"]["persistent"]; ok {
		t.Error("openvpn was asked to persist")
	}
	if dataOf(t, p)["connection-type"] != "password" {
		t.Error("openvpn's connection-type did not default to password")
	}
	empty := BuildProfile("org.freedesktop.NetworkManager.openvpn", "Office", "uuid-1", map[string]string{})
	if _, ok := dataOf(t, empty)["connection-type"]; ok {
		t.Error("an empty openvpn form was given a connection-type")
	}
}

func TestSecretKeysNeverLandInVPNData(t *testing.T) {
	for _, service := range typedPlugins {
		values := map[string]string{}
		for _, f := range fieldsForPlugin(service) {
			values[f.Key] = "value-of-" + f.Key
		}
		p := BuildProfile(service, "X", "uuid-1", values)
		data := dataOf(t, p)
		stored, _ := p["vpn"]["secrets"].Value().(map[string]string)
		for _, key := range SecretKeys(service) {
			if _, typed := values[key]; !typed {
				continue
			}
			if _, leaked := data[key]; leaked {
				t.Errorf("%s: secret %q stored in vpn.data, in the clear", service, key)
			}
			if stored[key] != "value-of-"+key {
				t.Errorf("%s: secret %q missing from vpn.secrets", service, key)
			}
			if data[key+"-flags"] != "0" {
				t.Errorf("%s: %s-flags = %q, want agent-owned 0", service, key, data[key+"-flags"])
			}
		}
		for key := range values {
			if !IsSecret(service, key) {
				if _, ok := data[key]; !ok {
					t.Errorf("%s: plain key %q missing from vpn.data", service, key)
				}
				if _, ok := stored[key]; ok {
					t.Errorf("%s: plain key %q stored as a secret", service, key)
				}
			}
		}
	}
}

func TestEmptyValuesAreNotWritten(t *testing.T) {
	p := BuildProfile("org.freedesktop.NetworkManager.openvpn", "X", "u", map[string]string{"remote": "a:1", "password": "", "username": ""})
	data := dataOf(t, p)
	for _, key := range []string{"password", "password-flags", "username"} {
		if _, ok := data[key]; ok {
			t.Errorf("empty %s written", key)
		}
	}
	if _, ok := p["vpn"]["secrets"]; ok {
		t.Error("an empty secrets dict written")
	}
}

func TestNoVPNIsEverCreatedDiallingItself(t *testing.T) {
	for _, kind := range []string{WireGuard, openconnect.ServiceType} {
		p := BuildProfile(kind, "X", "uuid-1", nil)
		if p["connection"]["autoconnect"].Value() != false {
			t.Errorf("%s autoconnects", kind)
		}
	}
}

func TestOwnershipReplacesThePermissionsAndNothingElse(t *testing.T) {
	p := BuildProfile(openconnect.ServiceType, "Work", "uuid-1", map[string]string{"gateway": "vpn.example.com"})
	p["connection"]["permissions"] = dbus.MakeVariant([]string{"user:bob", "user:carol"})
	before := maps.Clone(p["connection"])
	ownedBy(p, "alice")
	if got, _ := p["connection"]["permissions"].Value().([]string); !slices.Equal(got, []string{"user:alice:"}) {
		t.Errorf("permissions = %v", got)
	}
	delete(before, "permissions")
	after := maps.Clone(p["connection"])
	delete(after, "permissions")
	if len(before) != len(after) {
		t.Errorf("ownership touched other keys: %v -> %v", before, after)
	}
}

func TestAddressesSplitByFamilyWithSanePrefixes(t *testing.T) {
	v4, v6 := splitAddresses("10.0.0.2/24, fd00::2/64, 192.168.1.5")
	if !slices.Equal(v4, []address{{"10.0.0.2", 24}, {"192.168.1.5", 32}}) || !slices.Equal(v6, []address{{"fd00::2", 64}}) {
		t.Errorf("v4 = %v, v6 = %v", v4, v6)
	}
	v4, v6 = splitAddresses("not-an-address, 10.0.0.2/24")
	if len(v4) != 1 || len(v6) != 0 {
		t.Errorf("a non-address was kept: %v %v", v4, v6)
	}
	if v4, _ := splitAddresses("10.0.0.2/99"); !slices.Equal(v4, []address{{"10.0.0.2", 32}}) {
		t.Errorf("oversized prefix = %v", v4)
	}
	dns4, dns6 := splitServers("hello, 1.1.1.1 fd00::1")
	if !slices.Equal(dns4, []string{"1.1.1.1"}) || !slices.Equal(dns6, []string{"fd00::1"}) {
		t.Errorf("dns = %v %v", dns4, dns6)
	}
}

func TestASavedProfileReadsBackWithoutItsBookkeeping(t *testing.T) {
	p := BuildProfile(openconnect.ServiceType, "Work", "uuid-1", map[string]string{"gateway": "vpn.example.com", "protocol": "gp"})
	read := ReadProfileValues(p)
	if read["gateway"] != "vpn.example.com" {
		t.Errorf("gateway = %q", read["gateway"])
	}
	for key := range read {
		if strings.HasSuffix(key, "-flags") {
			t.Errorf("bookkeeping key %s on the form", key)
		}
	}
	if KindOf(p) != openconnect.ServiceType {
		t.Errorf("kind = %q", KindOf(p))
	}
	wg := BuildProfile(WireGuard, "Home", "uuid-1", map[string]string{"interface": "wg0"})
	if KindOf(wg) != WireGuard || ReadProfileValues(wg)["interface"] != "wg0" {
		t.Errorf("wireguard reads back as %q %v", KindOf(wg), ReadProfileValues(wg))
	}
	if KindOf(ConnectionDict{}) != "" {
		t.Error("a dict with no connection has a kind")
	}
}

func TestGeneratedKeysAreWireGuardShapedAndDerive(t *testing.T) {
	pair := GenerateKeyPair()
	for _, key := range []string{pair.Private, pair.Public} {
		raw, err := base64.StdEncoding.DecodeString(key)
		if len(key) != 44 || err != nil || len(raw) != 32 {
			t.Errorf("key %q is not 32 bytes of base64", key)
		}
	}
	if got, ok := PublicKeyFor(pair.Private); !ok || got != pair.Public {
		t.Errorf("public = %q, want %q", got, pair.Public)
	}
	if got, _ := PublicKeyFor("  " + pair.Private + "\n"); got != pair.Public {
		t.Error("surrounding whitespace from a paste broke the derivation")
	}
	if GenerateKeyPair().Private == pair.Private {
		t.Error("two generations made the same key")
	}
}

func TestAKnownVectorMatchesWgPubkey(t *testing.T) {
	// Checked against real `wg pubkey` (wireguard-tools 1.0.20260223).
	got, ok := PublicKeyFor("yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=")
	if !ok || got != "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=" {
		t.Errorf("public = %q", got)
	}
}

func TestAKeyThatIsNotAKeyDerivesNothing(t *testing.T) {
	for _, bad := range []string{"not base64 !!", base64.StdEncoding.EncodeToString(make([]byte, 16)), ""} {
		if got, ok := PublicKeyFor(bad); ok {
			t.Errorf("PublicKeyFor(%q) = %q", bad, got)
		}
	}
}

const wgConf = `[Interface]
# The tunnel's own end.
PrivateKey = 6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=
Address = 10.0.0.2/24, fd00::2/64
DNS = 10.0.0.1
MTU = 1420

[Peer]
PublicKey = Kx3AZBHm3vDJXPGRAJfvTvUEHY1c2Jw4qYE9nR6qEXY=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = vpn.example.com:51820
PersistentKeepalive = 25
`

func TestEveryFieldTheFormAsksForComesOutOfTheFile(t *testing.T) {
	values, ok := ParseWgQuick(wgConf, "wg0.conf")
	if !ok {
		t.Fatal("not read as a wg-quick config")
	}
	want := map[string]string{
		"interface":        "wg0",
		"private-key":      "6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=",
		"address":          "10.0.0.2/24, fd00::2/64",
		"dns":              "10.0.0.1",
		"peer-public-key":  "Kx3AZBHm3vDJXPGRAJfvTvUEHY1c2Jw4qYE9nR6qEXY=",
		"peer-allowed-ips": "0.0.0.0/0, ::/0",
		"peer-endpoint":    "vpn.example.com:51820",
		"peer-keepalive":   "25",
	}
	if !maps.Equal(values, want) {
		t.Errorf("values = %v\nwant %v", values, want)
	}
}

func TestAPresharedKeyIsReadWhenThereIsOne(t *testing.T) {
	withPSK := strings.Replace(wgConf, "AllowedIPs", "PresharedKey = 1oi/mVxLBOM4kRvxTOnQ8Nau6Fmy0OQ4pFm+Xn9zR0M=\nAllowedIPs", 1)
	values, _ := ParseWgQuick(withPSK, "wg0.conf")
	if values["peer-preshared-key"] != "1oi/mVxLBOM4kRvxTOnQ8Nau6Fmy0OQ4pFm+Xn9zR0M=" {
		t.Errorf("psk = %q", values["peer-preshared-key"])
	}
}

func TestOnlyTheFirstPeerIsTaken(t *testing.T) {
	two := wgConf + "\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = other.example.com:51821\n"
	values, _ := ParseWgQuick(two, "wg0.conf")
	if values["peer-endpoint"] != "vpn.example.com:51820" {
		t.Errorf("endpoint = %q", values["peer-endpoint"])
	}
}

func TestCommentsAndBlankLinesAreNotSettings(t *testing.T) {
	noisy := "[Interface]\n\n  # PrivateKey = not-this-one\nPrivateKey = 6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=\nAddress = 10.0.0.2/24 # the tunnel address\n"
	values, _ := ParseWgQuick(noisy, "wg0.conf")
	if values["private-key"] != "6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=" || values["address"] != "10.0.0.2/24" {
		t.Errorf("values = %v", values)
	}
}

func TestTheFileNameNamesTheInterface(t *testing.T) {
	for file, want := range map[string]string{
		"/home/alice/Downloads/work.conf":     "work",
		"wg0":                                 "wg0",
		"":                                    defaultInterface,
		"a name the kernel would refuse.conf": defaultInterface,
		"sixteen-chars-xx.conf":               defaultInterface,
	} {
		values, _ := ParseWgQuick(wgConf, file)
		if values["interface"] != want {
			t.Errorf("%q names %q, want %q", file, values["interface"], want)
		}
	}
}

func TestSomethingThatIsNotAWgQuickFileIsRefused(t *testing.T) {
	for _, text := range []string{
		"",
		"hello world",
		"[Interface]\nAddress = 10.0.0.2/24\n",
		"[Peer]\nPrivateKey = 6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=\n",
	} {
		if _, ok := ParseWgQuick(text, "wg0.conf"); ok {
			t.Errorf("read %q as a wg-quick config", text)
		}
	}
}
