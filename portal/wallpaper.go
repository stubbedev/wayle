package portal

import (
	"context"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/service/wallpaper"
	"github.com/stubbedev/wayle/shell/portaldialogs"
)

// wallpaperPortal maps SetWallpaperURI onto the shell's
// com.wayle.Wallpaper1, on every monitor; only file:// URIs are
// supported (wallpaper.rs).
type wallpaperPortal struct{ conn *dbus.Conn }

func wallpaperIface(conn *dbus.Conn) dbusx.Interface {
	return dbusx.Interface{
		Name:       "org.freedesktop.impl.portal.Wallpaper",
		Methods:    wallpaperPortal{conn},
		Properties: dbusx.Getters{"version": func() any { return uint32(1) }},
	}
}

// SetWallpaperURI sets the wallpaper, after the preview dialog when
// show-preview asks for one.
func (w wallpaperPortal) SetWallpaperURI(_ dbus.ObjectPath, _, _, uri string, options Vardict) (uint32, *dbus.Error) {
	path, ok := fileURIPath(uri)
	if !ok {
		warnf("wallpaper: only file:// URIs are supported: %s", uri)
		return ResponseOther, nil
	}
	ctx := context.Background()
	if preview, _ := optBool(options, "show-preview"); preview {
		set, err := portaldialogs.NewClient(w.conn).ConfirmWallpaper(ctx, uri)
		switch {
		case err != nil:
			warnf("wallpaper: preview prompt failed: %v", err)
			return ResponseOther, nil
		case !set:
			return ResponseCancelled, nil
		}
	}
	if err := wallpaper.NewClient(w.conn).SetWallpaper(ctx, path, ""); err != nil {
		warnf("wallpaper: set_wallpaper failed: %v", err)
		return ResponseOther, nil
	}
	return ResponseSuccess, nil
}

// fileURIPath decodes a file:// URI's path, dropping any authority
// (file://host/path) and undoing %XX escapes; other schemes, and a
// file URI with no path, are not paths.
func fileURIPath(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return "", false
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return "", false
	}
	return percentDecode(rest[slash:]), true
}

// percentDecode undoes %XX escapes, leaving a malformed one as is and
// replacing invalid UTF-8 as from_utf8_lossy does.
func percentDecode(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if b, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				out = append(out, byte(b))
				i += 2
				continue
			}
		}
		out = append(out, s[i])
	}
	return strings.ToValidUTF8(string(out), "�")
}
