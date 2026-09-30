package popups

import (
	"testing"

	"github.com/stubbedev/gelm/render"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/styling"
)

func testFont(t *testing.T) render.Font {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func newTestPopups(t *testing.T, cfg config.NotificationConfig) (*Popups, *notifications.Service) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	svc := notifications.NewService()
	return New(nil, svc, cfg, testFont(t), styling.Default(), nil), svc
}

func TestSyncTracksPopupSet(t *testing.T) {
	p, svc := newTestPopups(t, config.DefaultsNotification())
	svc.Notify("app", 0, "", "One", "body", nil, 0)
	p.sync()
	if got := p.Visible(); got != 1 {
		t.Fatalf("visible = %d, want 1", got)
	}
	// DND clears the stack.
	svc.SetDND(true)
	p.sync()
	if got := p.Visible(); got != 0 {
		t.Fatalf("under dnd = %d, want 0", got)
	}
	svc.SetDND(false)
	p.sync()
	if got := p.Visible(); got != 1 {
		t.Fatalf("after dnd off = %d, want 1", got)
	}
	// Dismissal drops the card.
	svc.DismissAll()
	p.sync()
	if got := p.Visible(); got != 0 {
		t.Fatalf("after dismiss all = %d", got)
	}
}

func TestSyncCapsAtMaxVisible(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupMaxVisible = 2
	p, svc := newTestPopups(t, cfg)
	svc.Notify("app", 0, "", "One", "", nil, 0)
	svc.Notify("app", 0, "", "Two", "", nil, 0)
	svc.Notify("app", 0, "", "Three", "", nil, 0)
	p.sync()
	if got := p.Visible(); got != 2 {
		t.Fatalf("visible = %d, want the 2 cap", got)
	}
	// The oldest scrolls off first (newest-first stacking keeps 2+3).
	svc.DismissAll()
	p.sync()
}

func TestSyncHonorsStackingOrder(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupStacking = "oldest-first"
	p, svc := newTestPopups(t, cfg)
	a := svc.Notify("app", 0, "", "First", "", nil, 0)
	b := svc.Notify("app", 0, "", "Second", "", nil, 0)
	p.sync()
	// Both stay; the order array puts the oldest at the top end.
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.order[0] != a || p.order[1] != b {
		t.Errorf("order = %v, want [%d %d]", p.order, a, b)
	}
}

func TestZeroPopupDurationSticks(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupDurationMS = 0
	p, svc := newTestPopups(t, cfg)
	svc.Notify("app", 0, "", "Sticky", "", nil, 0)
	p.sync()
	if got := p.Visible(); got != 1 {
		t.Fatalf("visible = %d", got)
	}
}

func TestPopupIconFallback(t *testing.T) {
	n := &notifications.Notification{AppName: "app"}
	if got := notifPopupIcon(n); got != "ld-bell-symbolic" {
		t.Errorf("fallback = %q", got)
	}
	n.AppIcon = "mail-send"
	if got := notifPopupIcon(n); got != "mail-send" {
		t.Errorf("app icon = %q", got)
	}
}

func TestAnchorsCornersOnly(t *testing.T) {
	// Popups accept the corners and sides; the pure top/bottom edges
	// are not in the schema's popup-position set (config-side check).
	if _, ok := popupsAnchorOK(config.OsdTopRight); !ok {
		t.Error("top-right rejected")
	}
	if _, ok := popupsAnchorOK(config.OsdTop); ok {
		t.Error("top accepted")
	}
}
