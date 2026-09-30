package main

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/shellipc"
)

// lockCommand is app.rs's Lock: lock the session through the running
// shell's lock screen (wayle/src/cli/lock.rs).
func lockCommand() *cli.Command {
	return &cli.Command{
		Name:  "lock",
		About: "Lock the session via Wayle's lock screen",
		Run: func(m *cli.Matches) error {
			conn, err := dbus.ConnectSessionBus()
			if conn != nil {
				defer func() { _ = conn.Close() }()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			return shellipc.LockCommand(ctx, conn, err, m.Stdout())
		},
	}
}
