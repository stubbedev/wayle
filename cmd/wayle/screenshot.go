package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbuscli"
	"github.com/stubbedev/wayle/shell/screenshot"
)

// screenshotUsage lists wayle/src/cli/screenshot's subcommands.
const screenshotUsage = "screenshot needs a command: region|output [NAME]|window"

// parseScreenshot maps the subcommand onto the daemon's (mode, target).
func parseScreenshot(args []string) (mode, target string, err error) {
	if len(args) == 0 {
		return "", "", errors.New(screenshotUsage)
	}
	switch args[0] {
	case "region", "window":
		if len(args) > 1 {
			return "", "", fmt.Errorf("screenshot %s takes no arguments", args[0])
		}
		return args[0], "", nil
	case "output":
		if len(args) > 2 {
			return "", "", errors.New("screenshot output takes at most one output name")
		}
		if len(args) == 2 {
			return screenshot.ModeOutput, args[1], nil
		}
		return screenshot.ModeOutput, "", nil
	}
	return "", "", fmt.Errorf("unknown screenshot command %q (%s)", args[0], screenshotUsage)
}

// runScreenshot is wayle/src/cli/screenshot: ask the running shell to
// capture, then report where the shot landed.
func runScreenshot(args []string, out io.Writer) error {
	mode, target, err := parseScreenshot(args)
	if err != nil {
		return err
	}
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
		_, _ = fmt.Fprintln(out, "Screenshot cancelled")
	} else {
		_, _ = fmt.Fprintf(out, "Screenshot saved to %s\n", path)
	}
	return nil
}
