package bar

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/network"
)

// startNetworkService brings up the NetworkManager service (the secret
// agent, VPNs, wifi) on its own system-bus connection; stop ends its
// watchers and closes the connection.
func startNetworkService() (*network.Service, func(), error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, nil, fmt.Errorf("system bus: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	svc, err := network.Start(ctx, conn)
	if err != nil {
		cancel()
		_ = conn.Close()
		return nil, nil, err
	}
	return svc, func() {
		cancel()
		_ = conn.Close()
	}, nil
}
