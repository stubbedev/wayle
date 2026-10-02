package portal

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/shell/portaldialogs"
)

// The interfaces whose whole job is one of the shell's native dialogs
// (com.wayle.PortalDialogs1): Access, Account, AppChooser and
// DynamicLauncher. No xdg-desktop-portal-gtk.

// dialogs carries the D-Bus methods of all four; their names do not
// collide, and each interface exports only its own.
type dialogs struct{ conn *dbus.Conn }

func (d dialogs) client() *portaldialogs.Client { return portaldialogs.NewClient(d.conn) }

// confirm maps a yes/no dialog onto a response: yes is success, no a
// cancel, a failed prompt (no shell) other.
func confirm(what string, yes bool, err error) uint32 {
	switch {
	case err != nil:
		warnf("%s: prompt failed: %v", what, err)
		return ResponseOther
	case !yes:
		return ResponseCancelled
	}
	return ResponseSuccess
}

func dialogIfaces(conn *dbus.Conn) []dbusx.Interface {
	version := func(v uint32) dbusx.Getters { return dbusx.Getters{"version": func() any { return v }} }
	d := dialogs{conn}
	launcher := version(1)
	// Applications and web apps.
	launcher["SupportedLauncherTypes"] = func() any { return uint32(1 | 2) }
	return []dbusx.Interface{
		{Name: "org.freedesktop.impl.portal.Access", Methods: accessMethods{d}, Properties: version(1)},
		{Name: "org.freedesktop.impl.portal.Account", Methods: accountMethods{d}, Properties: version(1)},
		{Name: "org.freedesktop.impl.portal.AppChooser", Methods: appChooserMethods{d}, Properties: version(2)},
		{Name: "org.freedesktop.impl.portal.DynamicLauncher", Methods: launcherMethods{d}, Properties: launcher},
	}
}

// accessMethods is org.freedesktop.impl.portal.Access (access.rs).
type accessMethods struct{ dialogs }

// AccessDialog shows a grant/deny prompt.
func (a accessMethods) AccessDialog(_ dbus.ObjectPath, _, _, title, subtitle, body string, options Vardict) (uint32, Vardict, *dbus.Error) {
	granted, err := a.client().Access(context.Background(), portaldialogs.AccessRequest{
		Title: title, Subtitle: subtitle, Body: body,
		GrantLabel: stringOr(options, "grant_label", "Allow"),
		DenyLabel:  stringOr(options, "deny_label", "Deny"),
		Icon:       stringOr(options, "icon", ""),
	})
	return confirm("access", granted, err), Vardict{}, nil
}

// accountMethods is org.freedesktop.impl.portal.Account (account.rs).
type accountMethods struct{ dialogs }

// GetUserInformation returns the user's id, real name and avatar once
// they agree to share them.
func (a accountMethods) GetUserInformation(_ dbus.ObjectPath, _, _ string, options Vardict) (uint32, Vardict, *dbus.Error) {
	shared, err := a.client().Account(context.Background(), stringOr(options, "reason", ""))
	if code := confirm("account", shared, err); code != ResponseSuccess {
		return code, Vardict{}, nil
	}
	info := currentUser()
	results := Vardict{"id": dbus.MakeVariant(info.id), "name": dbus.MakeVariant(info.name)}
	if info.image != "" {
		results["image"] = dbus.MakeVariant(info.image)
	}
	return ResponseSuccess, results, nil
}

type userInfo struct{ id, name, image string }

// currentUser is $USER (else the passwd login), the GECOS real name
// (else the id), and ~/.face as a file:// URI when it exists.
func currentUser() userInfo {
	info := userInfo{id: os.Getenv("USER")}
	if u, err := user.LookupId(strconv.Itoa(os.Getuid())); err == nil {
		if info.id == "" {
			info.id = u.Username
		}
		info.name = u.Name
	}
	if info.name == "" {
		info.name = info.id
	}
	if home, ok := os.LookupEnv("HOME"); ok {
		face := filepath.Join(home, ".face")
		if _, err := os.Stat(face); err == nil {
			info.image = "file://" + face
		}
	}
	return info
}

// appChooserMethods is org.freedesktop.impl.portal.AppChooser
// (appchooser.rs).
type appChooserMethods struct{ dialogs }

// ChooseApplication lets the user pick the handler.
func (a appChooserMethods) ChooseApplication(_ dbus.ObjectPath, _, _ string, choices []string, options Vardict) (uint32, Vardict, *dbus.Error) {
	choice, err := a.client().ChooseApplication(context.Background(), choices, stringOr(options, "content_type", ""), stringOr(options, "uri", ""))
	if code := confirm("appchooser", choice != "", err); code != ResponseSuccess {
		return code, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{"choice": dbus.MakeVariant(choice)}, nil
}

// UpdateChoices is accepted and ignored: the open chooser does not
// live-update.
func (appChooserMethods) UpdateChoices(_ dbus.ObjectPath, _ []string) *dbus.Error { return nil }

// launcherMethods is org.freedesktop.impl.portal.DynamicLauncher
// (dynamiclauncher.rs).
type launcherMethods struct{ dialogs }

// PrepareInstall confirms installing a launcher and echoes its name and
// icon back; the frontend writes the desktop file.
func (l launcherMethods) PrepareInstall(_ dbus.ObjectPath, _, _, name string, icon dbus.Variant, _ Vardict) (uint32, Vardict, *dbus.Error) {
	ok, err := l.client().ConfirmInstall(context.Background(), name, "")
	if code := confirm("dynamiclauncher", ok, err); code != ResponseSuccess {
		return code, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{"name": dbus.MakeVariant(name), "icon": icon}, nil
}

// RequestInstallToken grants the token the frontend mints.
func (launcherMethods) RequestInstallToken(_ string, _ Vardict) (uint32, *dbus.Error) {
	return ResponseSuccess, nil
}
