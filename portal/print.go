package portal

import (
	"context"
	"os"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/shell/printdialog"
)

// printPortal is org.freedesktop.impl.portal.Print over the shell's
// com.wayle.Print1 (print.rs): PreparePrint shows the dialog and
// replies the settings and a token, Print spools the document to the
// printer prepared under it.
type printPortal struct{ conn *dbus.Conn }

func printIface(conn *dbus.Conn) dbusx.Interface {
	return dbusx.Interface{
		Name:       "org.freedesktop.impl.portal.Print",
		Methods:    printPortal{conn},
		Properties: dbusx.Getters{"version": func() any { return uint32(1) }},
	}
}

// PreparePrint shows the print dialog. The page setup rides inside the
// settings, so page_setup is empty: an a{sv}, as the spec types it
// (Rust sent an a{ss}, which a typed lookup does not find).
func (p printPortal) PreparePrint(_ dbus.ObjectPath, _, _, title string, _, _, _ Vardict) (uint32, Vardict, *dbus.Error) {
	granted, settings, token, err := printdialog.NewClient(p.conn).Prepare(context.Background(), title)
	switch {
	case err != nil:
		warnf("print: prepare failed: %v", err)
		return ResponseOther, Vardict{}, nil
	case !granted:
		return ResponseCancelled, Vardict{}, nil
	}
	dict := Vardict{}
	for _, s := range settings {
		dict[s.Key] = dbus.MakeVariant(s.Value)
	}
	return ResponseSuccess, Vardict{
		"settings":   dbus.MakeVariant(dict),
		"page_setup": dbus.MakeVariant(Vardict{}),
		"token":      dbus.MakeVariant(token),
	}, nil
}

// Print spools fd to the prepared printer; a job not sent is other.
func (p printPortal) Print(_ dbus.ObjectPath, _, _, title string, fd dbus.UnixFD, options Vardict) (uint32, Vardict, *dbus.Error) {
	doc := os.NewFile(uintptr(fd), "document")
	defer func() { _ = doc.Close() }()
	token, _ := optU32(options, "token")
	sent, err := printdialog.NewClient(p.conn).Print(context.Background(), title, fd, token)
	if err != nil {
		warnf("print: spooling failed: %v", err)
		return ResponseOther, Vardict{}, nil
	}
	if !sent {
		return ResponseOther, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{}, nil
}
