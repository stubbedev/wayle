package portal

import (
	"context"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/internal/fileuri"
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
	path, ok := fileuri.Path(uri)
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
