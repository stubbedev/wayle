package portal

import (
	"context"
	"errors"
	"net/url"

	"github.com/godbus/dbus/v5"
)

// FileChooserIface is org.freedesktop.portal.FileChooser.
const FileChooserIface = "org.freedesktop.portal.FileChooser"

// FileFilter is one named filter of glob patterns.
type FileFilter struct {
	Name     string
	Patterns []string
}

// filterEntry is the portal's (sa(us)) filter: glob rules are type 0.
type filterEntry struct {
	Name  string
	Rules []filterRule
}

type filterRule struct {
	Kind    uint32
	Pattern string
}

// OpenFile asks the user for one file (GTK's FileDialog.open through
// the portal) and returns its local path. A dismissed chooser is
// ErrCancelled.
func OpenFile(ctx context.Context, conn *dbus.Conn, title string, filters ...FileFilter) (string, error) {
	opts := map[string]dbus.Variant{"modal": dbus.MakeVariant(true)}
	if len(filters) > 0 {
		entries := make([]filterEntry, 0, len(filters))
		for _, f := range filters {
			e := filterEntry{Name: f.Name}
			for _, p := range f.Patterns {
				e.Rules = append(e.Rules, filterRule{0, p})
			}
			entries = append(entries, e)
		}
		opts["filters"] = dbus.MakeVariant(entries)
	}
	results, err := call(ctx, conn, FileChooserIface+".OpenFile", opts, "", title)
	if err != nil {
		return "", err
	}
	uris, _ := results["uris"].Value().([]string)
	if len(uris) == 0 {
		return "", errors.New("portal: OpenFile returned no file")
	}
	u, err := url.Parse(uris[0])
	if err != nil || u.Scheme != "file" {
		return "", errors.New("portal: OpenFile returned a non-local file: " + uris[0])
	}
	return u.Path, nil
}
