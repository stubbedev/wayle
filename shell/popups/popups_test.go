package popups

import (
	"testing"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
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
	// DND keeps new cards off; the one already up stays.
	svc.SetDND(true)
	svc.Notify("app", 0, "", "Two", "", nil, 0)
	p.sync()
	if got := p.Visible(); got != 1 {
		t.Fatalf("under dnd = %d, want only the card shown before", got)
	}
	svc.SetDND(false)
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
	cfg.PopupStackingOrder = config.StackingOrderOldestFirst
	p, svc := newTestPopups(t, cfg)
	a := svc.Notify("app", 0, "", "First", "", nil, 0)
	b := svc.Notify("app", 0, "", "Second", "", nil, 0)
	p.sync()
	// Oldest-first appends each new card: the oldest sits on top.
	p.mu.Lock()
	got := p.stackOrder()
	p.mu.Unlock()
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("oldest-first order = %v, want [%d %d]", got, a, b)
	}
	// Newest-first (the default) prepends: the newest sits on top.
	p.cfg.PopupStackingOrder = config.StackingOrderNewestFirst
	p.mu.Lock()
	got = p.stackOrder()
	p.mu.Unlock()
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Errorf("newest-first order = %v, want [%d %d]", got, b, a)
	}
}

func TestHoverPausesTheCountdown(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupDuration = 40
	p, svc := newTestPopups(t, cfg)
	id := svc.Notify("app", 0, "", "Hover", "", nil, -1)
	p.sync()
	p.mu.Lock()
	hc, ok := p.cards[id].root.(*hoverCard)
	p.mu.Unlock()
	if !ok {
		t.Fatalf("card root = %T, want the hover wrapper under popup-hover-pause", p.cards[id].root)
	}
	hc.SetHovered(true)
	time.Sleep(80 * time.Millisecond)
	if got := len(svc.Popups()); got != 1 {
		t.Fatalf("hovered popup timed out: popups = %d", got)
	}
	hc.SetHovered(false)
	deadline := time.Now().Add(time.Second)
	for len(svc.Popups()) > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(svc.Popups()); got != 0 {
		t.Fatalf("popup survived the leave: popups = %d", got)
	}
}

func TestHoverPauseOffLeavesTheCardBare(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupHoverPause = false
	cfg.PopupDuration = 30
	p, svc := newTestPopups(t, cfg)
	id := svc.Notify("app", 0, "", "Bare", "", nil, -1)
	p.sync()
	p.mu.Lock()
	c := p.cards[id]
	p.mu.Unlock()
	if _, wrapped := c.root.(*hoverCard); wrapped || c.root != widget.Widget(c.box) {
		t.Fatalf("card root = %T, want the plain box with hover-pause off", c.root)
	}
}

func TestOutputPicksTheMonitor(t *testing.T) {
	dp, hdmi := &app.Output{Name: "DP-1"}, &app.Output{Name: "HDMI-A-1"}
	outs := []*app.Output{dp, hdmi}
	if got := Output(outs, config.PopupMonitor{}); got != dp {
		t.Errorf("primary = %v, want the first output", got)
	}
	if got := Output(outs, config.PopupOnConnector("HDMI-A-1")); got != hdmi {
		t.Errorf("connector = %v, want HDMI-A-1", got)
	}
	if got := Output(outs, config.PopupOnConnector("DP-9")); got != dp {
		t.Errorf("missing connector = %v, want the primary fallback", got)
	}
	if got := Output(nil, config.PopupMonitor{}); got != nil {
		t.Errorf("no outputs = %v, want nil", got)
	}
}

func TestZeroPopupDurationSticks(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupDuration = 0
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

func TestPopupAnchors(t *testing.T) {
	// notification_popup/methods.rs apply_position: corners take both
	// edges, the centered positions one.
	for pos, want := range map[config.PopupPosition]app.Anchor{
		config.PopupPositionTopRight:     app.AnchorTop | app.AnchorRight,
		config.PopupPositionBottomLeft:   app.AnchorBottom | app.AnchorLeft,
		config.PopupPositionTopCenter:    app.AnchorTop,
		config.PopupPositionBottomCenter: app.AnchorBottom,
		config.PopupPositionCenterLeft:   app.AnchorLeft,
		config.PopupPositionCenterRight:  app.AnchorRight,
	} {
		if got := popupAnchors(pos); got != want {
			t.Errorf("%s anchors = %v, want %v", pos, got, want)
		}
	}
	if got := popupAnchors("sideways"); got != app.AnchorTop|app.AnchorRight {
		t.Errorf("an unknown position = %v, want the top-right default", got)
	}
}

func TestSetConfigRebuildsTheStackWithTheNewLimits(t *testing.T) {
	cfg := config.DefaultsNotification()
	p, svc := newTestPopups(t, cfg)
	for range 3 {
		svc.Notify("app", 0, "", "hi", "", nil, 0)
	}
	p.sync()
	if got := p.Visible(); got != 3 {
		t.Fatalf("visible = %d, want 3 under the default limit", got)
	}
	cfg.PopupMaxVisible = 1
	p.SetConfig(cfg)
	if got := p.Visible(); got != 1 {
		t.Errorf("after a reload with popup-max-visible = 1: visible = %d", got)
	}
}
