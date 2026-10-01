package popups

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
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

func newTestPopups(t *testing.T, ncfg config.NotificationConfig) (*Popups, *notifications.Service) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	svc := notifications.NewService()
	cfg := config.Defaults()
	cfg.Notification = ncfg
	return New(nil, svc, cfg, testFont(t), styling.Default(), nil, nil), svc
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
	p.cfg.Notification.PopupStackingOrder = config.StackingOrderNewestFirst
	p.mu.Lock()
	got = p.stackOrder()
	p.mu.Unlock()
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Errorf("newest-first order = %v, want [%d %d]", got, b, a)
	}
}

// cardOf is the built card for id.
func cardOf(t *testing.T, p *Popups, id uint32) *card {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cards[id]
	if !ok {
		t.Fatalf("no card for %d", id)
	}
	c.root.Measure(widget.Constraints{Max: widget.Size{W: cardWidthPx, H: 600}})
	c.root.Arrange(render.Rect{W: cardWidthPx, H: 600})
	return c
}

// click presses and releases at the center of w through a router over
// the card.
func click(c *card, w interface{ Bounds() render.Rect }) {
	b := w.Bounds()
	at := widget.Point{X: b.X + b.W/2, Y: b.Y + b.H/2}
	r := &widget.Router{Root: c.root}
	r.Move(at)
	r.Press(widget.BTNLeft, at)
	r.Release(widget.BTNLeft, at)
}

// signals records the service's D-Bus emissions.
type signals struct {
	mu   sync.Mutex
	seen []string
}

func (s *signals) emit(member string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, strings.TrimPrefix(member, notifications.Interface+".")+fmt.Sprint(args...))
}

func (s *signals) count(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.seen {
		if strings.HasPrefix(m, prefix) {
			n++
		}
	}
	return n
}

func TestHoverPausesTheCountdown(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupDuration = 40
	p, svc := newTestPopups(t, cfg)
	id := svc.Notify("app", 0, "", "Hover", "", nil, -1)
	p.sync()
	c := cardOf(t, p, id)
	r := &widget.Router{Root: c.root}
	b := c.title.Bounds()
	r.Move(widget.Point{X: b.X + 1, Y: b.Y + 1})
	time.Sleep(80 * time.Millisecond)
	if got := len(svc.Popups()); got != 1 {
		t.Fatalf("hovered popup timed out: popups = %d", got)
	}
	r.Leave()
	deadline := time.Now().Add(time.Second)
	for len(svc.Popups()) > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(svc.Popups()); got != 0 {
		t.Fatalf("popup survived the leave: popups = %d", got)
	}
}

func TestHoverPauseOffKeepsCounting(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupHoverPause = false
	cfg.PopupDuration = 30
	p, svc := newTestPopups(t, cfg)
	id := svc.Notify("app", 0, "", "Bare", "", nil, -1)
	p.sync()
	c := cardOf(t, p, id)
	r := &widget.Router{Root: c.root}
	b := c.title.Bounds()
	r.Move(widget.Point{X: b.X + 1, Y: b.Y + 1})
	deadline := time.Now().Add(time.Second)
	for len(svc.Popups()) > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(svc.Popups()); got != 0 {
		t.Fatalf("a hovered card paused with hover-pause off: popups = %d", got)
	}
}

func TestCardContent(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.IconSource = config.IconSourceMapped
	cfg.PopupUrgencyBar = config.UrgencyBarThresholdCritical
	p, svc := newTestPopups(t, cfg)
	p.now = func() time.Time { return time.Now().Add(5*time.Minute + time.Second) }
	full := svc.NotifyHints("firefox", 0, "", "Download done", "a &amp; <b>b</b>", nil,
		map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(2))}, 0)
	bare := svc.Notify("", 0, "", "No app", "", nil, 0)
	p.sync()
	c := cardOf(t, p, full)
	if c.app.Text() != "firefox" || c.title.Text() != "Download done" || c.body.Text() != "a & b" || !c.body.Visible() {
		t.Errorf("card = %q / %q / %q", c.app.Text(), c.title.Text(), c.body.Text())
	}
	if c.time.Text() != i18n.T("notification-popup-time-minutes-ago", i18n.Str("minutes", "5")) {
		t.Errorf("time = %q", c.time.Text())
	}
	if !c.root.HasClass("critical") || !c.root.HasClass("urgency-bar") || !c.root.HasClass("notification-popup-card") {
		t.Errorf("classes = %v", c.root.Classes())
	}
	if c.root.HasClass("shadow") != cfg.PopupShadow {
		t.Error("the shadow class does not follow popup-shadow")
	}
	if c.actions.Visible() {
		t.Error("an action row without actions")
	}
	b := cardOf(t, p, bare)
	if b.app.Text() != i18n.T("notification-popup-unknown-app") || b.body.Visible() {
		t.Errorf("bare card: app %q body visible %v", b.app.Text(), b.body.Visible())
	}
	if b.root.HasClass("urgency-bar") || !b.root.HasClass("normal") {
		t.Errorf("a normal card under the default threshold: %v", b.root.Classes())
	}

	cfg.PopupShadow = !cfg.PopupShadow
	cfg.PopupUrgencyBar = config.UrgencyBarThresholdNone
	next := config.Defaults()
	next.Notification = cfg
	p.SetConfig(next)
	if c := cardOf(t, p, full); c.root.HasClass("shadow") != cfg.PopupShadow || c.root.HasClass("urgency-bar") {
		t.Errorf("after a reload: %v", c.root.Classes())
	}
}

func TestCardCloseBehavior(t *testing.T) {
	for _, tc := range []struct {
		behavior    config.PopupCloseBehavior
		keepHistory bool
	}{
		{config.PopupCloseBehaviorDismiss, true},
		{config.PopupCloseBehaviorRemove, false},
	} {
		cfg := config.DefaultsNotification()
		cfg.PopupCloseBehavior = tc.behavior
		p, svc := newTestPopups(t, cfg)
		id := svc.Notify("app", 0, "", "Close me", "", []string{"default", "Open"}, 0)
		var sig signals
		svc.SetEmitter(sig.emit)
		p.sync()
		c := cardOf(t, p, id)
		click(c, c.close)
		if len(svc.Popups()) != 0 {
			t.Errorf("%s: the popup stayed", tc.behavior)
		}
		if kept := len(svc.Notifications()) == 1; kept != tc.keepHistory {
			t.Errorf("%s: in history %v, want %v", tc.behavior, kept, tc.keepHistory)
		}
		if sig.count("ActionInvoked") != 0 {
			t.Errorf("%s: the close button invoked the default action", tc.behavior)
		}
		// The default action reaches the app under either behavior: it is
		// invoked before the close can take the notification away.
		open := svc.Notify("app", 0, "", "Open me", "", []string{"default", "Open"}, 0)
		p.sync()
		oc := cardOf(t, p, open)
		click(oc, oc.title)
		if sig.count("ActionInvoked"+fmt.Sprint(open, notifications.DefaultActionID)) != 1 {
			t.Errorf("%s: default action not invoked: %v", tc.behavior, sig.seen)
		}
	}
}

func TestCardActionsAndDefault(t *testing.T) {
	p, svc := newTestPopups(t, config.DefaultsNotification())
	id := svc.Notify("chat", 0, "", "Ping", "", []string{
		"default", "Open", "a", "One", "b", "Two", "c", "Three", "d", "Four",
	}, 0)
	plain := svc.Notify("chat", 0, "", "No default", "", nil, 0)
	var sig signals
	svc.SetEmitter(sig.emit)
	p.sync()
	c := cardOf(t, p, id)
	rows := c.actions.Children()
	if !c.actions.Visible() || len(rows) != 2 || len(rows[0].(*widget.Box).Children()) != 3 || len(rows[1].(*widget.Box).Children()) != 1 {
		t.Fatalf("action rows = %d, want 3 + 1 without the default", len(rows))
	}
	// A click on plain text invokes the default action.
	click(c, c.title)
	if sig.count("ActionInvoked"+fmt.Sprint(id, notifications.DefaultActionID)) != 1 || len(svc.Popups()) != 1 {
		t.Errorf("default: %v, popups %d", sig.seen, len(svc.Popups()))
	}
	// No default action: a click on the card does nothing.
	pc := cardOf(t, p, plain)
	click(pc, pc.title)
	if sig.count("ActionInvoked"+strconv.FormatUint(uint64(plain), 10)) != 0 {
		t.Error("a card without a default action invoked one")
	}

	// An action button invokes its action and dismisses.
	other := svc.Notify("chat", 0, "", "Reply", "", []string{"reply", "Reply"}, 0)
	p.sync()
	oc := cardOf(t, p, other)
	btn := oc.actions.Children()[0].(*widget.Box).Children()[0].(*widget.Button)
	click(oc, btn)
	if sig.count("ActionInvoked"+fmt.Sprint(other, "reply")) != 1 {
		t.Errorf("action: %v", sig.seen)
	}
	for _, n := range svc.Notifications() {
		if n.ID == other {
			t.Error("an invoked notification stayed in the history")
		}
	}
}

func TestPopupMargins(t *testing.T) {
	cfg := config.DefaultsNotification()
	cfg.PopupMarginX = config.Scale(1)
	cfg.PopupMarginY = config.Px(20)
	cfg.PopupPosition = config.PopupPositionTopRight
	if got := popupMargins(cfg, 2); got != (app.Margins{Top: 20, Right: 24}) {
		t.Errorf("top-right = %+v, want y 20px on top, x 1x12x2 on the right", got)
	}
	cfg.PopupPosition = config.PopupPositionBottomCenter
	if got := popupMargins(cfg, 1); got != (app.Margins{Bottom: 20}) {
		t.Errorf("bottom-center = %+v, want only the bottom", got)
	}
	cfg.PopupPosition = config.PopupPositionCenterLeft
	if got := popupMargins(cfg, 1); got != (app.Margins{Left: 12}) {
		t.Errorf("center-left = %+v, want only the left", got)
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
	next := config.Defaults()
	next.Notification = cfg
	p.SetConfig(next)
	if got := p.Visible(); got != 1 {
		t.Errorf("after a reload with popup-max-visible = 1: visible = %d", got)
	}
}
