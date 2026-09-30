package bar

import (
	"testing"

	"github.com/stubbedev/wayle/service/network"
)

func TestAVPNRowShowsItsReasonOverItsState(t *testing.T) {
	failed := network.VPN{State: network.VPNFailed, Detail: "authentication failed"}
	if got := vpnRowCaption(failed); got != "Authentication failed" {
		t.Errorf("caption = %q", got)
	}
	gateway := network.VPN{State: network.VPNFailed, Detail: "  Invalid username or password "}
	if got := vpnRowCaption(gateway); got != "Invalid username or password" {
		t.Errorf("the gateway's own wording was changed: %q", got)
	}
	for state, want := range map[network.VPNState]string{
		network.VPNConnected:    "Connected",
		network.VPNConnecting:   "Connecting...",
		network.VPNDisconnected: "Disconnected",
		network.VPNFailed:       "Connection failed",
	} {
		if got := vpnRowCaption(network.VPN{State: state}); got != want {
			t.Errorf("%v caption = %q, want %q", state, got, want)
		}
	}
}

func TestVPNRowGlyphs(t *testing.T) {
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
}

func TestSentenceTouchesOnlyTheFirstLetter(t *testing.T) {
	if got := sentence("éclair dropped"); got != "Éclair dropped" {
		t.Errorf("sentence = %q", got)
	}
	if got := sentence("   "); got != "" {
		t.Errorf("blank = %q", got)
	}
}

func TestNoNetworkServiceMeansNoVPNSection(t *testing.T) {
	if vpnSection(ModuleContext{}, nil, 14) != nil {
		t.Error("a VPN section without NetworkManager")
	}
}
