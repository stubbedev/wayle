package bar

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// sessionCardTree walks the section into its named boxes and buttons.
func sessionCardTree(t *testing.T, card widget.Widget) (row, info, avatar, meta, actions *widget.Box, btns []*widget.Button) {
	t.Helper()
	cardBox, ok := card.(*widget.Box)
	if !ok || !cardBox.HasClass("card") || !cardBox.HasClass("dashboard-card") {
		t.Fatalf("the section root = %T %v, want the card dashboard-card", card, card)
	}
	row = classedBox(t, cardBox, "dashboard-user-session")
	info = classedBox(t, row, "user-info")
	if len(info.Children()) != 2 {
		t.Fatalf("user-info children = %d, want the avatar and the meta box", len(info.Children()))
	}
	avatar, ok = info.Children()[0].(*widget.Box)
	if !ok || !avatar.HasClass("user-avatar") {
		t.Fatalf("user-info's first child = %T, want the user-avatar tile", info.Children()[0])
	}
	meta, ok = info.Children()[1].(*widget.Box)
	if !ok {
		t.Fatalf("user-info's second child = %T, want the meta box", info.Children()[1])
	}
	actions = classedBox(t, row, "session-actions")
	walkTree(actions, func(w widget.Widget) bool {
		if b, ok := w.(*widget.Button); ok && b.HasClass("session-btn") {
			btns = append(btns, b)
		}
		return true
	})
	return row, info, avatar, meta, actions, btns
}

// The user session matches the Rust tree: card > session row > user-info
// (avatar tile over user-meta/user-name) with the actions pinned to the
// row's end, the buttons in the IconButton shape.
func TestUserSessionMatchesTheRustTree(t *testing.T) {
	t.Setenv("USER", "wayle")
	t.Setenv("HOME", t.TempDir()) // no ~/.face: the user glyph
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	card := userSessionSection(ctx)
	row, info, avatar, meta, actions, btns := sessionCardTree(t, card)

	if got := len(row.Children()); got != 3 {
		t.Errorf("session row children = %d, want user-info, the expanding filler, and the actions", got)
	}
	// The avatar rides the tile; the name rides user-meta.
	if len(avatar.Children()) != 1 {
		t.Fatalf("user-avatar children = %d, want the glyph", len(avatar.Children()))
	}
	glyph, isIcon := avatar.Children()[0].(*widget.Icon)
	if !isIcon || glyph.Name() != "ld-user-symbolic" {
		t.Fatalf("the avatar = %v, want the ld-user glyph", avatar.Children()[0])
	}
	// The cascade inks the glyph (.user-avatar image): no tint.
	if glyph.Tint() != 0 {
		t.Errorf("the avatar glyph carries a tint %#08x", uint32(glyph.Tint()))
	}
	name, isLabel := meta.Children()[0].(*widget.Label)
	if len(meta.Children()) != 1 || !isLabel || !name.HasClass("user-name") {
		t.Fatalf("user-meta = %v, want the user-name label", meta.Children())
	}
	if got := name.Text(); got != "wayle" {
		t.Errorf("username = %q, want $USER", got)
	}
	if got := name.Color(); got != 0 {
		t.Errorf("the user name carries a programmatic color %#08x", uint32(got))
	}

	// One icon button per configured action, in order, with i18n
	// tooltips and no Go hover fills.
	want := []config.SessionAction{config.SessionActionLock, config.SessionActionLogOut, config.SessionActionReboot, config.SessionActionPowerOff}
	keys := []string{"dropdown-dashboard-lock", "dropdown-dashboard-logout", "dropdown-dashboard-reboot", "dropdown-dashboard-power-off"}
	if len(btns) != len(want) {
		t.Fatalf("session buttons = %d, want %d", len(btns), len(want))
	}
	for i, b := range btns {
		spec := sessionButtonFor(want[i], cfg.Dashboard)
		if !b.HasClass("icon") || !b.HasClass("session-btn") {
			t.Errorf("button %d carries classes %v, want icon + session-btn", i, b.Classes())
		}
		if got := b.TooltipText(); got != i18n.T(keys[i]) {
			t.Errorf("button %d tooltip = %q, want %q", i, got, i18n.T(keys[i]))
		} else if spec.tooltip != got {
			t.Errorf("button %d tooltip disagrees with sessionButtonFor", i)
		}
		if b.BgHover != 0 || b.BgPressed != 0 {
			t.Errorf("button %d carries Go hover fills %#08x/%#08x", i, uint32(b.BgHover), uint32(b.BgPressed))
		}
		walkTree(b, func(c widget.Widget) bool {
			if icon, ok := c.(*widget.Icon); ok && icon.Tint() != 0 {
				t.Errorf("button %d's glyph carries a tint %#08x", i, uint32(icon.Tint()))
			}
			return true
		})
	}
	// Negative: the actions box is spacing-0 (its border-spacing is CSS).
	if got := actions.Spacing(); got != 0 {
		t.Errorf("session-actions spacing = %d, want 0", got)
	}
	if got := info.Spacing(); got != 0 {
		t.Errorf("user-info spacing = %d, want 0", got)
	}
}

// The configured actions drive the buttons: a single-action session
// builds one button with that action's tooltip, and an empty list
// builds none.
func TestUserSessionButtonsFollowTheConfig(t *testing.T) {
	t.Setenv("USER", "wayle")
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Dashboard.UserSession.Actions = []config.SessionAction{config.SessionActionLock}
	ctx := newTestContext(t, cfg)
	_, _, _, _, actions, btns := sessionCardTree(t, userSessionSection(ctx))
	if len(btns) != 1 {
		t.Fatalf("buttons = %d, want 1", len(btns))
	}
	if got := btns[0].TooltipText(); got != i18n.T("dropdown-dashboard-lock") {
		t.Errorf("tooltip = %q, want %q", got, i18n.T("dropdown-dashboard-lock"))
	}
	if got := actions.Spacing(); got != 0 {
		t.Errorf("session-actions spacing = %d", got)
	}

	cfg.Dashboard.UserSession.Actions = nil
	ctx = newTestContext(t, cfg)
	_, _, _, _, _, btns = sessionCardTree(t, userSessionSection(ctx))
	if len(btns) != 0 {
		t.Errorf("an empty action list built %d buttons", len(btns))
	}
}

// writeFace writes a 4x2 PNG at ~/.face, wide enough to exercise the
// center crop.
func writeFace(t *testing.T, home string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.White)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(home, ".face"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// The avatar tile is the face's background image (the Rust avatar's
// inline url()); without a face file it carries none and the glyph
// shows.
func TestUserSessionAvatarTakesTheFaceAsItsBackground(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USER", "wayle")
	t.Setenv("HOME", home)
	writeFace(t, home)
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	_, _, avatar, _, _, _ := sessionCardTree(t, userSessionSection(ctx))
	face := filepath.Join(home, ".face")
	if !strings.Contains(avatar.InlineStyle(), `url('`+face+`')`) {
		t.Errorf("the tile carries %q, want the face's url", avatar.InlineStyle())
	}

	// No face file, no background image: the glyph shows.
	os.Remove(face)
	ctx = newTestContext(t, cfg)
	_, _, avatar, _, _, _ = sessionCardTree(t, userSessionSection(ctx))
	if avatar.InlineStyle() != "" {
		t.Errorf("without a face the tile carries %q", avatar.InlineStyle())
	}
}
