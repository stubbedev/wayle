package main

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/dbuscli"
	"github.com/stubbedev/wayle/shell/screenshot"
)

// screenshotCommand is wayle/src/cli/screenshot/commands.rs: each
// subcommand asks the running shell to capture, then reports where the
// shot landed.
func screenshotCommand() *cli.Command {
	return &cli.Command{
		Name:  "screenshot",
		About: "Screenshot capture commands",
		Subcommands: []*cli.Command{
			{Name: "region", About: "Capture a drag-selected region", Run: captureScreenshot(screenshot.ModeRegion)},
			{
				Name:  "output",
				About: "Capture a whole output (the focused output when NAME is omitted)",
				Args: []*cli.Arg{
					{ID: "name", Help: "Output connector name (e.g. DP-1)"},
				},
				Run: captureScreenshot(screenshot.ModeOutput),
			},
			{Name: "window", About: "Capture the active window", Run: captureScreenshot(screenshot.ModeWindow)},
		},
	}
}

// captureScreenshot runs one capture mode; output's optional NAME is
// the target.
func captureScreenshot(mode string) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		target, _ := cli.Value[string](m, "name")
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			return fmt.Errorf("Failed to connect to D-Bus session bus: %w", err) //nolint:staticcheck // the Rust CLI's message
		}
		defer func() { _ = conn.Close() }()
		// No timeout: a region capture waits for the user's drag.
		path, err := screenshot.NewClient(conn).Capture(context.Background(), mode, target)
		if err != nil {
			return dbuscli.FormatError("Screenshot", "capture screenshot", err)
		}
		if path == "" {
			_, _ = fmt.Fprintln(m.Stdout(), "Screenshot cancelled")
		} else {
			_, _ = fmt.Fprintf(m.Stdout(), "Screenshot saved to %s\n", path)
		}
		return nil
	}
}
