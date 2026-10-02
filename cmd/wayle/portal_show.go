package main

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/dbuscli"
	"github.com/stubbedev/wayle/shell/filechooser"
	"github.com/stubbedev/wayle/shell/portaldialogs"
	"github.com/stubbedev/wayle/shell/printdialog"
	"github.com/stubbedev/wayle/shell/screenshot"
	"github.com/stubbedev/wayle/shell/sharepicker"
)

// The `wayle portal show <dialog>` previews (cli/portal/show.rs): each
// calls the shell service the real backend delegates to, with
// placeholder arguments, and prints the answer — `cancelled` for an
// empty one. The calls wait on the user, so they carry no timeout.

// showDialog connects to the session bus around one preview; show
// returns the lines to print, or the service's label and operation
// for the error text.
func showDialog(show func(ctx context.Context, conn *dbus.Conn) (lines []string, service, op string, err error)) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			return cliMessage("Failed to connect to D-Bus session bus: " + err.Error())
		}
		defer func() { _ = conn.Close() }()
		lines, service, op, err := show(context.Background(), conn)
		if err != nil {
			return dbuscli.FormatError(service, op, err)
		}
		for _, l := range lines {
			_, _ = fmt.Fprintln(m.Stdout(), l)
		}
		return nil
	}
}

// orCancelled prints a result, or `cancelled` for none.
func orCancelled(results ...string) []string {
	var out []string
	for _, r := range results {
		if r != "" {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return []string{"cancelled"}
	}
	return out
}

// verdict prints one of two words.
func verdict(ok bool, yes, no string) []string {
	if ok {
		return []string{yes}
	}
	return []string{no}
}

func showFileChooser(m *cli.Matches) error {
	return showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
		c := filechooser.NewClient(conn)
		var uris []string
		var err error
		if m.Flag("save") {
			uris, err = c.Save(ctx, filechooser.SaveRequest{Title: "Preview: Save File", CurrentName: "untitled.txt"})
		} else {
			uris, err = c.Open(ctx, filechooser.OpenRequest{Title: "Preview: Open File", Multiple: m.Flag("multiple"), Directory: m.Flag("directory")})
		}
		return orCancelled(uris...), "FileChooser", "show file chooser", err
	})(m)
}

func showScreenshot(m *cli.Matches) error {
	mode, _ := cli.Value[string](m, "mode")
	target, _ := cli.Value[string](m, "target")
	return showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
		path, err := screenshot.NewClient(conn).Capture(ctx, mode, target)
		return orCancelled(path), "Screenshot", "capture screenshot", err
	})(m)
}

var showColor = showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
	r, g, b, err := screenshot.NewClient(conn).PickColor(ctx)
	return []string{fmt.Sprintf("rgb(%.3f, %.3f, %.3f)", r, g, b)}, "Screenshot", "pick color", err
})

func showScreenCast(m *cli.Matches) error {
	return showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
		// No window list: the picker with no pre-seeded sources.
		selection, err := sharepicker.NewClient(conn).Pick(ctx, "", m.Flag("allow_token"), m.Flag("multiple"))
		return orCancelled(selection), "SharePicker", "show share picker", err
	})(m)
}

var showPrint = showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
	granted, settings, token, err := printdialog.NewClient(conn).Prepare(ctx, "Preview: Print")
	if !granted {
		return []string{"cancelled"}, "Print", "show print dialog", err
	}
	return []string{fmt.Sprintf("prepared (token %d, %d settings)", token, len(settings))}, "Print", "show print dialog", err
})

var showAccess = showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
	granted, err := portaldialogs.NewClient(conn).Access(ctx, portaldialogs.AccessRequest{
		Title:      "Preview: Access",
		Subtitle:   "An application is requesting access",
		Body:       "This is a preview of the generic access prompt.",
		GrantLabel: "Allow",
		DenyLabel:  "Deny",
		Icon:       "dialog-password-symbolic",
	})
	return verdict(granted, "granted", "denied"), "Access", "show access prompt", err
})

var showAccount = showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
	shared, err := portaldialogs.NewClient(conn).Account(ctx, "This is a preview of the account-sharing consent prompt.")
	return verdict(shared, "shared", "declined"), "Account", "show account prompt", err
})

var showAppChooser = showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
	id, err := portaldialogs.NewClient(conn).ChooseApplication(ctx, nil, "text/plain", "")
	return orCancelled(id), "AppChooser", "show app chooser", err
})

var showDynamicLauncher = showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
	ok, err := portaldialogs.NewClient(conn).ConfirmInstall(ctx, "Preview Launcher", "application-x-executable")
	return verdict(ok, "approved", "rejected"), "DynamicLauncher", "show install confirmation", err
})

func showWallpaper(m *cli.Matches) error {
	uri, _ := cli.Value[string](m, "uri")
	return showDialog(func(ctx context.Context, conn *dbus.Conn) ([]string, string, string, error) {
		ok, err := portaldialogs.NewClient(conn).ConfirmWallpaper(ctx, uri)
		return verdict(ok, "accepted", "declined"), "Wallpaper", "show wallpaper preview", err
	})(m)
}
