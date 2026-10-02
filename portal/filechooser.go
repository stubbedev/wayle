package portal

import (
	"context"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/internal/fileuri"
	"github.com/stubbedev/wayle/shell/filechooser"
)

// fileChooser is org.freedesktop.impl.portal.FileChooser over the
// shell's com.wayle.FileChooser1 (filechooser.rs): OpenFile, SaveFile,
// and SaveFiles as a folder pick.
type fileChooser struct{ conn *dbus.Conn }

func fileChooserIface(conn *dbus.Conn) dbusx.Interface {
	return dbusx.Interface{
		Name:       "org.freedesktop.impl.portal.FileChooser",
		Methods:    fileChooser{conn},
		Properties: dbusx.Getters{"version": func() any { return uint32(3) }},
	}
}

func (f fileChooser) client() *filechooser.Client { return filechooser.NewClient(f.conn) }

// OpenFile opens existing files or a directory.
func (f fileChooser) OpenFile(_ dbus.ObjectPath, _, _, title string, options Vardict) (uint32, Vardict, *dbus.Error) {
	multiple, _ := optBool(options, "multiple")
	directory, _ := optBool(options, "directory")
	uris, err := f.client().Open(context.Background(), filechooser.OpenRequest{
		Title: title, Multiple: multiple, Directory: directory,
		Filters: fileFilters(options), CurrentFolder: currentFolder(options),
	})
	return urisResponse("open", uris, err)
}

// SaveFile chooses a save destination.
func (f fileChooser) SaveFile(_ dbus.ObjectPath, _, _, title string, options Vardict) (uint32, Vardict, *dbus.Error) {
	uris, err := f.client().Save(context.Background(), filechooser.SaveRequest{
		Title: title, CurrentName: stringOr(options, "current_name", ""),
		Filters: fileFilters(options), CurrentFolder: currentFolder(options),
	})
	return urisResponse("save", uris, err)
}

// SaveFiles picks a folder and replies one URI per requested file
// name inside it. The names are escaped into the URI (the Rust join
// left a name with a space or '#' an invalid URI).
func (f fileChooser) SaveFiles(_ dbus.ObjectPath, _, _, title string, options Vardict) (uint32, Vardict, *dbus.Error) {
	picked, err := f.client().Open(context.Background(), filechooser.OpenRequest{
		Title: title, Directory: true, CurrentFolder: currentFolder(options),
	})
	if err != nil || len(picked) == 0 {
		return urisResponse("folder pick", nil, err)
	}
	folder := strings.TrimRight(picked[0], "/")
	files, _ := options["files"].Value().([][]byte)
	uris := make([]string, 0, len(files))
	for _, name := range files {
		uris = append(uris, folder+"/"+fileuri.Encode(cString(name), ""))
	}
	return urisResponse("save files", uris, nil)
}

// urisResponse is the chosen URIs as a reply: none is a cancel, a
// failed dialog (no shell) other.
func urisResponse(what string, uris []string, err error) (uint32, Vardict, *dbus.Error) {
	switch {
	case err != nil:
		warnf("filechooser: %s failed: %v", what, err)
		return ResponseOther, Vardict{}, nil
	case len(uris) == 0:
		return ResponseCancelled, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{"uris": dbus.MakeVariant(uris)}, nil
}

// fileFilters reads the filters option, a(sa(us)).
func fileFilters(options Vardict) []filechooser.Filter {
	var filters []filechooser.Filter
	if v, ok := options["filters"]; ok {
		if err := dbus.Store([]any{v.Value()}, &filters); err != nil {
			warnf("filechooser: malformed filters: %v", err)
			return nil
		}
	}
	return filters
}

// currentFolder reads the current_folder option, a NUL-terminated ay.
func currentFolder(options Vardict) string {
	b, _ := options["current_folder"].Value().([]byte)
	return cString(b)
}

// cString is a portal byte string: trailing NULs dropped, invalid
// UTF-8 replaced as from_utf8_lossy does.
func cString(b []byte) string {
	return strings.ToValidUTF8(strings.TrimRight(string(b), "\x00"), "�")
}
