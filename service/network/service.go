package network

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// Service is the network service beyond the bar's status read
// (service.rs's NetworkService): saved profiles, the wifi device, the
// secret agent, and the VPNs.
type Service struct {
	// Settings is the live list of saved profiles.
	Settings *Settings
	// Wifi is the wireless device, nil when there is none.
	Wifi *Wifi
	// Agent is the secret agent's prompt state: the credential prompt
	// NM is blocked on, answered with Submit or Cancel.
	Agent *Agent
	// VPN is every VPN profile and its live state.
	VPN *VPNService
}

// Start brings the service up on conn (the system bus) until ctx ends.
// The secret agent is served before anything can activate: a VPN that
// needs a password has nowhere to ask otherwise. openconnect VPNs sign
// in natively (NativeSignIn).
func Start(ctx context.Context, conn *dbus.Conn) (*Service, error) {
	return start(ctx, conn, NativeSignIn, RequestBudget, defaultResumeTiming)
}

func start(ctx context.Context, conn *dbus.Conn, signIn SignIn, budget time.Duration, timing resumeTiming) (*Service, error) {
	n := nm{conn: conn}
	settings, err := newSettings(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize network service: cannot initialize Settings: %w", err)
	}
	svc := &Service{Settings: settings, Agent: NewAgent()}
	if device, ok := findWifiDevice(ctx, n); ok {
		svc.Wifi = &Wifi{nm: n, settings: settings, Device: device}
	}
	if err := serveAgent(ctx, conn, svc.Agent, signIn, budget); err != nil {
		return nil, err
	}
	vpn, err := newVPNService(ctx, n, settings, svc.Agent, signIn, timing)
	if err != nil {
		return nil, err
	}
	svc.VPN = vpn
	return svc, nil
}
