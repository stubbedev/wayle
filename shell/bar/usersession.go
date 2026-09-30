package bar

import (
	"image"
	"os"
	"path/filepath"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/imagedecode"
)

// avatarPx is the user avatar's logical box in the session row.
const avatarPx = 32

// sessionButton is one session action's glyph, tooltip, and command
// (user_session/factory.rs make_session_btn's per-action arguments).
type sessionButton struct {
	icon    string
	tooltip string
	command string
}

// sessionButtonFor maps an action onto its button; the commands come
// from the dashboard's dropdown-*-command keys.
func sessionButtonFor(action config.SessionAction, cfg config.DashboardConfig) sessionButton {
	switch action {
	case config.SessionLock:
		return sessionButton{"ld-lock-symbolic", "Lock", cfg.LockCommand}
	case config.SessionLogout:
		return sessionButton{"ld-log-out-symbolic", "Log Out", cfg.LogoutCommand}
	case config.SessionReboot:
		return sessionButton{"ld-refresh-cw-symbolic", "Reboot", cfg.RebootCommand}
	default:
		return sessionButton{"ld-power-symbolic", "Power Off", cfg.PowerOffCommand}
	}
}

// sessionUsername is $USER, "user" when unset (dashboard mod.rs).
func sessionUsername() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return "user"
}

// facePath is ~/.face; empty without $HOME.
func facePath() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".face")
}

// loadAvatar rasterizes the face image into the avatar box, center-
// cropped to a square the way the Rust avatar's background-image
// covers it. Nil when the file is absent or undecodable; the row then
// shows the user glyph.
func loadAvatar(path string) *render.Icon {
	if path == "" {
		return nil
	}
	img, err := imagedecode.Open(path)
	if err != nil {
		return nil
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	square := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	square.Max = square.Min.Add(image.Pt(side, side))
	if sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		img = sub.SubImage(square)
	}
	icon, err := render.IconFromImage(img, avatarPx, avatarPx)
	if err != nil {
		return nil
	}
	return icon
}

// userSessionSection is the dashboard's user-session card
// (user_session/mod.rs): the avatar (~/.face, else the user glyph) and
// the username on the left, one icon button per configured action on
// the right. The face is read when the dropdown opens, which is when
// the Rust watcher's change would next be seen.
func userSessionSection(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	cfg := ctx.Config.Dashboard
	row := widget.NewBox(widget.Row, 10, 0)
	row.AddClass("dashboard-user-session")

	var avatar widget.Widget
	if face := loadAvatar(facePath()); face != nil {
		avatar = widget.NewIcon(face)
	} else {
		glyph := widget.NewThemeIcon("ld-user-symbolic", avatarPx)
		glyph.SetTint(ctx.Style.fg)
		avatar = glyph
	}
	row.Append(avatar, false)
	name := widget.NewLabel(font, px, sessionUsername(), ctx.Style.fg)
	name.AddClass("user-name")
	row.Append(name, true)

	actions := widget.NewBox(widget.Row, 4, 0)
	actions.AddClass("session-actions")
	for _, action := range cfg.SessionActions {
		spec := sessionButtonFor(action, cfg)
		glyph := widget.NewThemeIcon(spec.icon, int(px))
		glyph.SetTint(ctx.Style.fg)
		button := widget.NewButton(glyph, 6, 6)
		button.AddClass("session-btn")
		button.SetTooltip(spec.tooltip)
		if ctx.Style != nil {
			button.BgHover = ctx.Style.buttonBgHover
			button.BgPressed = ctx.Style.buttonBgActive
		}
		command := spec.command
		// process::run_if_set: an empty command is a no-op.
		button.OnClick = func() { spawnCommand(command) }
		actions.Append(button, false)
	}
	row.Append(actions, false)
	return row
}
