package network

import (
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/wayle/service/network/openconnect"
	"github.com/stubbedev/wayle/service/network/openconnect/openconnecttest"
)

// The agent's withdrawal and budget contracts against the real
// sign-in, as the Rust tests run them: a GlobalProtect profile that has
// signed in before, pointed at a gateway that takes the connection and
// never answers.

func serveNative(t *testing.T, budget time.Duration) *agentFixture {
	t.Helper()
	// The sign-in caches passwords under $XDG_STATE_HOME/wayle/vpn.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f := startFakeNM(t)
	conn := f.client()
	agent := NewAgent()
	must(t, serveAgent(t.Context(), conn, agent, NativeSignIn, budget))
	f.registered(t)
	return &agentFixture{nm: f, agent: agent, name: conn.Names()[0]}
}

func signedInBefore(uuid, gateway string) ConnectionDict {
	return vpnConnection(uuid, "Work", openconnect.ServiceType, map[string]string{
		"gateway":               gateway,
		"protocol":              "gp",
		openconnect.UsernameKey: "alice",
	})
}

func TestANativeSignInNetworkManagerWithdrawsStopsAndKeepsThePassword(t *testing.T) {
	fx := serveNative(t, RequestBudget)
	openconnect.RememberPassword("withdrawn", "hunter2")
	gateway := openconnecttest.NewSilentGateway(t)
	pending := fx.ask(signedInBefore("withdrawn", gateway.Addr), 1, "vpn")
	if !gateway.Reached() {
		t.Fatal("the sign-in never got going")
	}
	fx.nm.cancelSecrets(fx.name, settingPath(1), "vpn")
	if a := await(t, pending); errName(a.err) != errAgentCanceled {
		t.Fatalf("err = %v", a.err)
	}
	if _, ok := fx.agent.Failure(); ok {
		t.Error("a withdrawn sign-in reported a failure nobody was waiting for")
	}
	if pw, _ := openconnect.RememberedPassword("withdrawn"); pw != "hunter2" {
		t.Errorf("a withdrawn sign-in cost the password: %q", pw)
	}
}

func TestANativeSignInThatRunsOutOfTimeSaysWhyAndKeepsThePassword(t *testing.T) {
	fx := serveNative(t, 300*time.Millisecond)
	openconnect.RememberPassword("out-of-time", "hunter2")
	gateway := openconnecttest.NewSilentGateway(t)
	_, err := fx.nm.getSecrets(t.Context(), fx.name, signedInBefore("out-of-time", gateway.Addr), settingPath(1), "vpn", nil, flagAllowInteraction)
	if errName(err) != errUserCanceled {
		t.Fatalf("err = %v", err)
	}
	failure, ok := fx.agent.Failure()
	if !ok || failure.UUID != "out-of-time" || !strings.Contains(failure.Reason, "did not finish") {
		t.Errorf("failure = %+v %v", failure, ok)
	}
	if pw, _ := openconnect.RememberedPassword("out-of-time"); pw != "hunter2" {
		t.Errorf("running out of time cost the password: %q", pw)
	}
}

func TestWithdrawingANativeSignInLeavesTheWifiPromptAlone(t *testing.T) {
	fx := serveNative(t, RequestBudget)
	openconnect.RememberPassword("withdraw-one", "hunter2")
	gateway := openconnecttest.NewSilentGateway(t)
	vpn := fx.ask(signedInBefore("withdraw-one", gateway.Addr), 1, "vpn")
	if !gateway.Reached() {
		t.Fatal("the sign-in never got going")
	}
	wifi := fx.ask(wifiConnection("home-wifi", "Home", []byte("home")), 2, "802-11-wireless-security")
	fx.shown(t)
	fx.nm.cancelSecrets(fx.name, settingPath(1), "vpn")
	if a := await(t, vpn); errName(a.err) != errAgentCanceled {
		t.Fatalf("vpn err = %v", a.err)
	}
	if req, ok := fx.agent.Request(); !ok || req.UUID != "home-wifi" {
		t.Fatalf("the wifi prompt went down with the VPN's request: %+v", req)
	}
	fx.agent.Submit(map[string]string{"psk": "correct horse"})
	if a := await(t, wifi); a.err != nil {
		t.Fatal(a.err)
	}
}
