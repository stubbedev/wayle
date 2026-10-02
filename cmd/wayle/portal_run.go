package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/portal"
	"github.com/stubbedev/wayle/shell/sharepicker"
)

// runPortal is `wayle portal` / `wayle portal run` (cli/portal/
// backend.rs): the backend serves until SIGTERM or SIGINT, then clears
// the live sessions and exits 0; a failure to start is exit 1.
func runPortal(m *cli.Matches) error {
	fail := func(err error) error {
		_, _ = fmt.Fprintf(m.Stderr(), "portal backend failed: %v\n", err)
		return cli.ExitError{Code: 1}
	}
	cfg, err := config.Open()
	if cfg == nil {
		return fail(fmt.Errorf("cannot load configuration: %w", err))
	}
	if err != nil {
		// Hot reload failed to start; the loaded config still serves.
		_, _ = fmt.Fprintf(m.Stderr(), "portal: config hot reload unavailable: %v\n", err)
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fail(fmt.Errorf("cannot connect to session bus: %w", err))
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	stop, err := portal.New(conn, cfg).Serve()
	if err != nil {
		return fail(err)
	}
	<-ctx.Done()
	stop()
	return nil
}

// runSharePicker is `wayle portal share-picker` (cli/portal/
// share_picker.rs), the xdg-desktop-portal-hyprland custom picker: it
// forwards XDPH_WINDOW_SHARING_LIST to the shell's picker and prints
// only the `[SELECTION]` line on stdout, which xdph parses; a cancel
// prints nothing.
func runSharePicker(m *cli.Matches) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		_, _ = fmt.Fprintf(m.Stderr(), "Failed to connect to D-Bus session bus: %v\n", err)
		return cli.ExitError{Code: 1}
	}
	defer func() { _ = conn.Close() }()
	// The legacy single-select path: never multi-select.
	selection, err := sharepicker.NewClient(conn).Pick(context.Background(), os.Getenv("XDPH_WINDOW_SHARING_LIST"), m.Flag("allow_token"), false)
	if err != nil {
		_, _ = fmt.Fprintln(m.Stderr(), dbusError("SharePicker", "open share picker", err, false))
		return cli.ExitError{Code: 1}
	}
	if selection != "" {
		_, _ = fmt.Fprintf(m.Stdout(), "[SELECTION]%s\n", selection)
	}
	return nil
}
