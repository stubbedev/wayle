// Command wayle-lock is a single-token `wayle lock` (wayle-lock.rs):
// the same dispatch as its own executable, for contexts that exec one
// token without splitting on whitespace - a systemd ExecStart=, an idle
// daemon, a compositor spawn - so locking needs no wrapper script.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/shellipc"
)

func main() {
	conn, err := dbus.ConnectSessionBus()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	err = shellipc.LockCommand(ctx, conn, err, os.Stdout)
	cancel()
	if conn != nil {
		_ = conn.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
