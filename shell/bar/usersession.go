package bar

import (
	"os"
	"path/filepath"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/spawn"
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
// from the dashboard's dropdown-*-command keys and the tooltips are
// the factory's i18n keys.
func sessionButtonFor(action config.SessionAction, cfg config.DashboardConfig) sessionButton {
	switch action {
	case config.SessionActionLock:
		return sessionButton{"ld-lock-symbolic", i18n.T("dropdown-dashboard-lock"), cfg.DropdownLockCommand}
	case config.SessionActionLogOut:
		return sessionButton{"ld-log-out-symbolic", i18n.T("dropdown-dashboard-logout"), cfg.DropdownLogoutCommand}
	case config.SessionActionReboot:
		return sessionButton{"ld-refresh-cw-symbolic", i18n.T("dropdown-dashboard-reboot"), cfg.DropdownRebootCommand}
	default:
		return sessionButton{"ld-power-symbolic", i18n.T("dropdown-dashboard-power-off"), cfg.DropdownPoweroffCommand}
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

// setAvatar paints the face onto the tile as its background image —
// the Rust avatar's `background-image: url(file://…)` inline style —
// cover-fit inside the rounded box. The tile shows the user glyph
// when the file is absent.
func setAvatar(tile *widget.Box) {
	path := facePath()
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	tile.SetInlineStyle("background-image: url('" + path + "');")
}

// userSessionSection is the dashboard's user-session card
// (user_session/mod.rs): the card over the session row — the avatar
// (~/.face, else the user glyph) and the username in .user-info, one
// icon button per configured action in .session-actions pinned to the
// row's end. The face is read when the dropdown opens, which is when
// the Rust watcher's change would next be seen. The stylesheet inks
// everything through the classes (user-avatar image, user-name,
// button.icon.session-btn image), so nothing carries a tint here.
func userSessionSection(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	cfg := ctx.Config.Dashboard
	card := widget.NewBox(widget.Column, 0, 0)
	card.AddClass("card", "dashboard-card")
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("dashboard-user-session")

	info := widget.NewBox(widget.Row, 0, 0)
	info.AddClass("user-info")
	tile := widget.NewBox(widget.Row, 0, 0)
	tile.AddClass("user-avatar")
	setAvatar(tile)
	tile.Append(widget.NewThemeIcon("ld-user-symbolic", avatarPx), false)
	info.AppendAligned(tile, false, widget.AlignStart)
	meta := widget.NewBox(widget.Column, 0, 0)
	name := widget.NewLabel(font, px, sessionUsername(), 0)
	name.AddClass("user-name")
	meta.Append(name, false)
	info.AppendAligned(meta, false, widget.AlignCenter)
	row.Append(info, false)

	// session-actions is hexpand with halign End: the info keeps its
	// natural size and the expanding filler pins the buttons to the
	// row's end.
	row.Append(widget.NewBox(widget.Row, 0, 0), true)
	actions := widget.NewBox(widget.Row, 0, 0)
	actions.AddClass("session-actions")
	for _, action := range cfg.UserSession.Actions {
		spec := sessionButtonFor(action, cfg)
		glyph := widget.NewThemeIcon(spec.icon, int(px))
		// button.icon.session-btn: the IconButton template's class
		// carries the pointer cursor, the hover shade, and the glyph
		// ink; no Go hover fills.
		button := widget.NewButton(glyph, 6, 6)
		button.AddClass("icon", "session-btn")
		button.SetTooltip(spec.tooltip)
		command := spec.command
		// process::run_if_set: an empty command is a no-op.
		button.OnClick = func() { _ = spawn.Quiet(command) }
		actions.Append(button, false)
	}
	row.Append(actions, false)
	card.Append(row, false)
	return card
}
