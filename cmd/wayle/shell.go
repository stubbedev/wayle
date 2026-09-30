package main

import (
	"fmt"
	"io"
	"os"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/logging"
	"github.com/stubbedev/wayle/service/shellipc"
	"github.com/stubbedev/wayle/shell/bar"
)

// shellCommand is app.rs's Shell: the desktop shell in the foreground.
func shellCommand() *cli.Command {
	return &cli.Command{
		Name:  "shell",
		About: "Run the desktop shell in the foreground",
		Run: func(*cli.Matches) error {
			// wayle-shell's tracing_init: its own daily file, echoed to
			// stdout.
			closer, err := logging.Setup("wayle-shell", os.Stdout)
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()
			return runShell(os.Stderr, bar.Run)
		},
	}
}

// runShell is wayle_shell::run's single-instance guard: a second shell
// says so and exits successfully instead of fighting over the bus names.
func runShell(stderr io.Writer, start func() error) error {
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		running, _ := shellipc.IsRunning(conn)
		_ = conn.Close()
		if running {
			fmt.Fprintln(stderr, "Wayle shell is already running")
			return nil
		}
	}
	return start()
}
