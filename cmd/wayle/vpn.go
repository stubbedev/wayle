package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/shellipc"
)

// vpnCommand is wayle/src/cli/vpn.rs: the browser's answer to a
// GlobalProtect SAML sign-in. Not something to run by hand: wayle
// registers a desktop entry for the globalprotectcallback: scheme
// pointing at `wayle vpn sso-callback`, so the identity provider's
// final redirect reaches the sign-in waiting inside the shell.
func vpnCommand() *cli.Command {
	return &cli.Command{
		Name:  "vpn",
		About: "VPN commands",
		Subcommands: []*cli.Command{
			{
				Name:      "sso-callback",
				About:     "Hand a browser sign-in callback to the running shell",
				LongAbout: "Hand a browser sign-in callback to the running shell\n\nRegistered as the handler for the `globalprotectcallback:` URI scheme; not normally run by hand.",
				Args: []*cli.Arg{
					{ID: "uri", Required: true, Help: "The `globalprotectcallback:` URI the browser was sent to"},
				},
				Run: func(m *cli.Matches) error {
					uri, _ := cli.Value[string](m, "uri")
					conn, err := dbus.ConnectSessionBus()
					if err != nil {
						return fmt.Errorf("D-Bus session unavailable: %w", err)
					}
					defer func() { _ = conn.Close() }()
					return ssoCallback(conn, uri, m.Stdout())
				},
			},
		},
	}
}

// ssoCallback forwards a globalprotectcallback: URI to the running
// shell. It fails when the shell is not running or no sign-in was
// waiting for a callback.
func ssoCallback(conn *dbus.Conn, uri string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shellipc.VPNSSOCallback(ctx, conn, uri); err != nil {
		return fmt.Errorf("VPN sign-in callback failed: %w", err)
	}
	fmt.Fprintln(out, "Sign-in completed; you can close the browser tab.")
	return nil
}
