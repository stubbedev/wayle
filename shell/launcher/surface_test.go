package launcher

import (
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/launcheripc"
	engine "github.com/stubbedev/wayle/service/launcher"
)

// fakeLoop is the UI loop: Invoke queues, pump runs on the test
// goroutine, as the real loop runs everything on one goroutine.
type fakeLoop struct {
	mu    sync.Mutex
	queue []func()
}

func (l *fakeLoop) invoke(fn func()) {
	l.mu.Lock()
	l.queue = append(l.queue, fn)
	l.mu.Unlock()
}

func (l *fakeLoop) pump() {
	for {
		l.mu.Lock()
		q := l.queue
		l.queue = nil
		l.mu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, fn := range q {
			fn()
		}
	}
}

// fakeWindow records what the surface does to its layer.
type fakeWindow struct {
	cfg     app.LayerConfig
	closed  bool
	focused widget.Widget
	sizes   [][2]uint32
}

func (w *fakeWindow) Close()                   { w.closed = true }
func (w *fakeWindow) SetFocus(f widget.Widget) { w.focused = f }
func (w *fakeWindow) SetSize(width, height uint32) error {
	w.sizes = append(w.sizes, [2]uint32{width, height})
	return nil
}

type fakeTimer struct {
	d       time.Duration
	fn      func()
	stopped bool
}

type harness struct {
	t       *testing.T
	loop    *fakeLoop
	s       *Surface
	cfg     *config.Config
	windows []*fakeWindow
	timers  []*fakeTimer
	hooks   []string
	copied  []string
	mods    engine.MouseModifiers
	nextID  uint64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, loop: &fakeLoop{}, cfg: config.Defaults()}
	h.cfg.Launcher.History.Enable = false
	// Surfaces close at once here; TestSurfacePlaysItsExit animates.
	h.cfg.Animations.Enabled = false
	h.s = New(Deps{
		Invoke: h.loop.invoke,
		After: func(d time.Duration, fn func()) func() {
			tm := &fakeTimer{d: d, fn: fn}
			h.timers = append(h.timers, tm)
			return func() { tm.stopped = true }
		},
		Config: func() *config.Config { return h.cfg },
		Open: func(cfg app.LayerConfig) (Window, error) {
			w := &fakeWindow{cfg: cfg}
			h.windows = append(h.windows, w)
			return w, nil
		},
		Font:         face,
		Ink:          render.RGB(255, 255, 255),
		MonitorWidth: func() int { return 2000 },
		Copy:         func(text string) { h.copied = append(h.copied, text) },
		HeldMods:     func() engine.MouseModifiers { return h.mods },
		FireHook: func(command string, ctx engine.HookContext) {
			if command != "" {
				h.hooks = append(h.hooks, command+":"+ctx.Entry)
			}
		},
	})
	return h
}

// open starts a session and returns its reply channel.
func (h *harness) open(opts launcheripc.SessionOptions, replace bool, rows ...string) (*launcheripc.Session, <-chan launcheripc.ServerFrame) {
	h.t.Helper()
	ch := make(chan []string, 1)
	if len(rows) > 0 {
		ch <- rows
	}
	close(ch)
	h.nextID++
	sess, reply := launcheripc.NewSession(h.nextID, opts, replace, ch)
	h.s.Open(sess)
	return sess, reply
}

// until pumps the loop until cond holds.
func (h *harness) until(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.loop.pump()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *harness) key(spec string) bool {
	h.t.Helper()
	accel, err := app.ParseAccel(spec)
	if err != nil {
		h.t.Fatal(err)
	}
	return h.s.keyCaptured(accel)
}

// frame pumps the loop until the session's terminal frame arrives.
func (h *harness) frame(reply <-chan launcheripc.ServerFrame) launcheripc.ServerFrame {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.loop.pump()
		select {
		case f := <-reply:
			h.loop.pump()
			return f
		default:
		}
		if time.Now().After(deadline) {
			h.t.Fatal("no terminal frame")
		}
		time.Sleep(time.Millisecond)
	}
}

func dmenu() launcheripc.SessionOptions { return launcheripc.SessionOptions{Dmenu: true} }

func TestDmenuSessionFiltersAndAccepts(t *testing.T) {
	h := newHarness(t)
	_, reply := h.open(dmenu(), false, "alpha", "beta", "gamma")
	h.until("the rows", func() bool { return h.s.cur != nil && h.s.model.len() == 3 })
	if len(h.windows) != 1 {
		t.Fatalf("windows = %d", len(h.windows))
	}
	w := h.windows[0]
	if w.cfg.Namespace != "wayle-launcher" || w.cfg.Keyboard != app.KeyboardOnDemand || w.cfg.Anchor != 0 || w.cfg.Layer != app.LayerOverlay {
		t.Errorf("layer = %+v", w.cfg)
	}
	if w.focused != widget.Widget(h.s.cur.views.entry) || h.s.sel != 0 {
		t.Error("the entry is not focused or the first row not selected")
	}
	h.s.cur.views.entry.SetText("ga")
	h.until("the filter", func() bool { return h.s.model.len() == 1 })
	if h.s.model.textAt(0) != "gamma" {
		t.Errorf("filtered = %q", h.s.model.textAt(0))
	}
	if !h.key("Return") {
		t.Fatal("Return is bound to accept")
	}
	h.loop.pump()
	f := h.frame(reply)
	if f.Type != launcheripc.FrameResult || f.Code != 0 || len(f.Selected) != 1 || f.Selected[0].Text != "gamma" || f.Filter != "ga" {
		t.Errorf("result = %+v", f)
	}
	h.until("the session to end", func() bool { return h.s.cur == nil })
	if !w.closed {
		t.Error("the surface stayed mapped")
	}
	if h.key("Return") {
		t.Error("a key reached a closed session")
	}
}

func TestUnboundKeysReachTheEntryAndCancelCloses(t *testing.T) {
	h := newHarness(t)
	_, reply := h.open(dmenu(), false, "one")
	h.until("the rows", func() bool { return h.s.model.len() == 1 })
	if h.key("a") {
		t.Error("a plain letter was captured instead of typed")
	}
	h.key("Escape")
	if f := h.frame(reply); f.Type != launcheripc.FrameCancelled || f.Code != 1 {
		t.Errorf("cancel = %+v", f)
	}
}

func TestBusyReplaceAndClientGone(t *testing.T) {
	h := newHarness(t)
	_, first := h.open(dmenu(), false, "a")
	h.until("the first session", func() bool { return h.s.cur != nil })
	_, busy := h.open(dmenu(), false, "b")
	h.loop.pump()
	if f := h.frame(busy); f.Type != launcheripc.FrameBusy {
		t.Errorf("a second session without -replace = %+v", f)
	}
	second, _ := h.open(dmenu(), true, "c")
	h.loop.pump()
	if f := h.frame(first); f.Type != launcheripc.FrameCancelled || f.Code != 1 {
		t.Errorf("the displaced session = %+v", f)
	}
	if h.s.cur == nil || h.s.cur.sess != second || !h.windows[0].closed {
		t.Fatal("-replace did not take over")
	}
	// A stale ClientGone leaves the live session alone; its own ends it.
	h.s.ClientGone(1)
	h.loop.pump()
	if h.s.cur == nil {
		t.Fatal("another connection's ClientGone closed the session")
	}
	h.s.ClientGone(second.ID)
	h.loop.pump()
	if h.s.cur != nil || !h.windows[1].closed {
		t.Error("ClientGone did not close its session")
	}
}

func TestDumpRepliesWithTheFilteredListUnmapped(t *testing.T) {
	h := newHarness(t)
	opts := dmenu()
	opts.Dump = true
	filter := "e"
	opts.Filter = &filter
	_, reply := h.open(opts, false, "one", "two", "three")
	h.until("the matches", func() bool { return len(h.timers) > 0 })
	if len(h.windows) != 0 {
		t.Error("-dump mapped the surface")
	}
	// Each match list restarts the debounce; the last one replies.
	last := h.timers[len(h.timers)-1]
	if last.d != dumpDebounce {
		t.Errorf("debounce = %v", last.d)
	}
	h.until("the filtered matches", func() bool { return h.s.model.len() == 2 })
	last = h.timers[len(h.timers)-1]
	last.fn()
	f := h.frame(reply)
	if f.Type != launcheripc.FrameDump || len(f.Items) != 2 || f.Items[0] != "one" || f.Items[1] != "three" {
		t.Errorf("dump = %+v", f)
	}
}

func TestErrorDialogClosesOnAnyBinding(t *testing.T) {
	h := newHarness(t)
	msg := "<b>no network</b>"
	_, reply := h.open(launcheripc.SessionOptions{ErrorMessage: &msg}, false)
	h.loop.pump()
	v := h.s.cur.views
	if v.inputRow.Visible() || v.frame.Visible() || !v.message.Visible() || v.message.Text() != "no network" {
		t.Errorf("dialog: input %v list %v message %q", v.inputRow.Visible(), v.frame.Visible(), v.message.Text())
	}
	if h.s.cur.act != nil {
		t.Error("a dialog started an engine")
	}
	h.key("Escape")
	if f := h.frame(reply); f.Type != launcheripc.FrameResult || f.Code != 0 {
		t.Errorf("dialog close = %+v", f)
	}
}

func TestNoUsableModesIsAMenuError(t *testing.T) {
	h := newHarness(t)
	hook := "notify"
	_, reply := h.open(launcheripc.SessionOptions{Modes: []string{"no-such-mode"}, OnMenuError: &hook}, false)
	h.loop.pump()
	if f := h.frame(reply); f.Type != launcheripc.FrameCancelled || f.Code != 1 {
		t.Errorf("no modes = %+v", f)
	}
	if len(h.windows) != 0 || h.s.cur != nil || len(h.hooks) != 1 {
		t.Errorf("windows %d hooks %v", len(h.windows), h.hooks)
	}
}

func TestRowClickAcceptsTheClickedRow(t *testing.T) {
	h := newHarness(t)
	_, reply := h.open(dmenu(), false, "alpha", "beta", "gamma")
	h.until("the rows", func() bool { return h.s.model.len() == 3 })
	// A middle click is bound to nothing.
	h.s.rowPressed(2, engine.MouseMiddle, 1)
	if h.s.sel != 0 {
		t.Error("an unbound button moved the selection")
	}
	h.s.rowPressed(1, engine.MousePrimary, 1)
	if f := h.frame(reply); len(f.Selected) != 1 || f.Selected[0].Text != "beta" {
		t.Errorf("click result = %+v", f)
	}
}

func TestWheelMovesTheSelection(t *testing.T) {
	h := newHarness(t)
	h.open(dmenu(), false, "a", "b", "c")
	h.until("the rows", func() bool { return h.s.model.len() == 3 })
	if !h.s.listScrolled(1) || h.s.sel != 1 {
		t.Errorf("wheel down: claimed and sel %d", h.s.sel)
	}
	h.s.listScrolled(-1)
	if h.s.sel != 0 {
		t.Errorf("wheel up: sel %d", h.s.sel)
	}
	h.mods = engine.MouseAlt
	if h.s.listScrolled(1) {
		t.Error("Alt+wheel is not a bound scroll")
	}
}

func TestMultiSelectTogglesAndAcceptsThePicked(t *testing.T) {
	h := newHarness(t)
	opts := dmenu()
	opts.MultiSelect = true
	_, reply := h.open(opts, false, "a", "b", "c")
	h.until("the rows", func() bool { return h.s.model.len() == 3 && h.s.multi.enabled })
	h.key("Shift+Return") // picks a, moves to b
	h.key("Down")         // c
	h.key("Shift+Return") // picks c
	if len(h.s.multi.picked) != 2 {
		t.Fatalf("picked = %v", h.s.multi.picked)
	}
	h.key("Return")
	f := h.frame(reply)
	if len(f.Selected) != 2 || f.Selected[0].Text != "a" || f.Selected[1].Text != "c" {
		t.Errorf("multi result = %+v", f)
	}
}

func TestDecisionHooks(t *testing.T) {
	h := newHarness(t)
	accepted, canceled := "acc", "can"
	opts := dmenu()
	opts.OnEntryAccepted, opts.OnMenuCanceled = &accepted, &canceled
	_, reply := h.open(opts, false, "alpha")
	h.until("the rows", func() bool { return h.s.model.len() == 1 })
	h.key("Return")
	if len(h.hooks) != 1 || h.hooks[0] != "acc:alpha" {
		t.Errorf("hooks = %v", h.hooks)
	}
	h.frame(reply)
	h.hooks = nil
	h.open(opts, false, "beta")
	h.until("the second session", func() bool { return h.s.cur != nil && h.s.model.len() == 1 })
	h.key("Escape")
	if len(h.hooks) != 1 || h.hooks[0] != "can:beta" {
		t.Errorf("cancel hooks = %v", h.hooks)
	}
}

func TestMoveSelectionCyclesOnlyUnderCycle(t *testing.T) {
	h := newHarness(t)
	h.open(dmenu(), false, "a", "b", "c")
	h.until("the rows", func() bool { return h.s.model.len() == 3 })
	h.s.cur.ui.cycle = false
	h.s.moveSelection(-1)
	if h.s.sel != 0 {
		t.Errorf("clamped up = %d", h.s.sel)
	}
	h.s.cur.ui.cycle = true
	h.s.moveSelection(-1)
	if h.s.sel != 2 {
		t.Errorf("cycled up = %d", h.s.sel)
	}
	h.s.moveSelection(5)
	if h.s.sel != 2 {
		t.Errorf("a page step clamps even under cycle: %d", h.s.sel)
	}
}

func TestLocationAnchorsAndWidth(t *testing.T) {
	for loc, want := range map[config.LauncherLocation]app.Anchor{
		config.LauncherCenter:    0,
		config.LauncherNorth:     app.AnchorTop,
		config.LauncherSouthEast: app.AnchorBottom | app.AnchorRight,
		config.LauncherWest:      app.AnchorLeft,
	} {
		if a, _ := locationAnchors(loc, 0, 0); a != want {
			t.Errorf("%s anchors = %v", loc, a)
		}
	}
	// Offsets sign from the anchor; a centered axis ignores its own.
	if _, m := locationAnchors(config.LauncherSouthEast, 10, 20); m != (app.Margins{Right: -10, Bottom: -20}) {
		t.Errorf("south-east margins = %+v", m)
	}
	if _, m := locationAnchors(config.LauncherNorth, 10, 20); m != (app.Margins{Top: 20}) {
		t.Errorf("north margins = %+v", m)
	}
	h := newHarness(t)
	ui := uiSettings{width: 600}
	if h.s.resolveWidth(ui) != 600 {
		t.Error("no -width keeps the configured width")
	}
	ui.widthOverride = &launcheripc.Width{Unit: launcheripc.WidthPercent, Value: 50}
	if got := h.s.resolveWidth(ui); got != 1000 {
		t.Errorf("50%% of 2000 = %d", got)
	}
	ui.widthOverride = &launcheripc.Width{Unit: launcheripc.WidthPixels, Value: 0}
	if h.s.resolveWidth(ui) != 1 {
		t.Error("a zero width floors at 1")
	}
	ui.widthOverride = &launcheripc.Width{Unit: launcheripc.WidthChars, Value: 10}
	if got := h.s.resolveWidth(ui); got < 10 || got%10 != 0 {
		t.Errorf("10 chars = %d", got)
	}
}

func TestRowDisplayColumnsAndEllipsize(t *testing.T) {
	d := rowDisplay{columns: []uint32{2, 3, 9}, separator: "|"}
	if got := d.applyColumns("a|b|c"); got != "b c" {
		t.Errorf("columns = %q", got)
	}
	if (rowDisplay{}).applyColumns("a|b") != "a|b" {
		t.Error("no columns keeps the text")
	}
	if (rowDisplay{ellipsize: "start"}).mode() != widget.EllipsizeStart || (rowDisplay{ellipsize: "x"}).mode() != widget.EllipsizeEnd {
		t.Error("ellipsize modes")
	}
}

func TestMatchModel(t *testing.T) {
	var m matchModel
	items := []engine.Item{engine.NewItem("Firefox"), {Display: "<b>Term</b>", MatchText: "Term", Flags: engine.FlagMarkup}}
	items[0].MatchText = "Firefox"
	m.update(items, []uint32{1, 0})
	if m.textAt(0) != "Term" || m.textAt(1) != "Firefox" || m.textAt(5) != "" {
		t.Errorf("texts %q %q", m.textAt(0), m.textAt(1))
	}
	if pos, ok := m.findPosition("FIRE"); !ok || pos != 1 {
		t.Errorf("find = %d %v", pos, ok)
	}
	if got := m.texts(); len(got) != 2 || got[0] != "<b>Term</b>" {
		t.Errorf("dump texts keep the display: %v", got)
	}
}

// TestAcceptBindingAcceptsTheRowItLandedOn pins on_mouse: an accept
// bound alone (no select binding beside it) still accepts the clicked
// row, not the one selected when the press came.
func TestAcceptBindingAcceptsTheRowItLandedOn(t *testing.T) {
	h := newHarness(t)
	_, reply := h.open(dmenu(), false, "alpha", "beta", "gamma")
	h.until("the rows", func() bool { return h.s.model.len() == 3 })
	h.s.cur.mouse = compileMouse([]engine.Binding{{Action: "me-accept-entry", Keys: "MousePrimary"}})
	h.s.rowPressed(2, engine.MousePrimary, 1)
	if f := h.frame(reply); len(f.Selected) != 1 || f.Selected[0].Text != "gamma" {
		t.Errorf("accept-only click = %+v", f)
	}
}

// TestALateEventFromAnEndedSessionIsDropped pins the sink guard: an
// engine event that arrives after its session ended never reaches the
// next session's list.
func TestALateEventFromAnEndedSessionIsDropped(t *testing.T) {
	h := newHarness(t)
	_, first := h.open(dmenu(), false, "a")
	h.until("the first session", func() bool { return h.s.model.len() == 1 })
	stale := &sessionSink{s: h.s, a: h.s.cur}
	h.key("Escape")
	h.frame(first)
	h.open(dmenu(), false, "b", "c")
	h.until("the second session", func() bool { return h.s.model.len() == 2 })
	stale.onMatches(engineMatches{items: []engine.Item{engine.NewItem("x")}, matched: []uint32{0}})
	stale.onAction(engine.ActionClose{})
	if h.s.model.len() != 2 || h.s.cur == nil {
		t.Errorf("a stale event reached the new session: %d rows, live %v", h.s.model.len(), h.s.cur != nil)
	}
}

// TestSurfacePlaysItsExit pins the enter and hide_animated: a session
// opens its surface entering with the launcher transition; when it
// ends the surface stays mapped through its exit and closes when that
// lands, while a replacing session already has its own.
func TestSurfacePlaysItsExit(t *testing.T) {
	h := newHarness(t)
	h.cfg.Animations.Enabled = true
	zoom := config.AnimationZoom
	h.cfg.Animations.Launcher.Exit = &zoom
	_, _ = h.open(dmenu(), false, "a")
	h.until("the first session", func() bool { return h.s.cur != nil && h.s.cur.rev != nil })
	first := h.s.cur
	if !first.rev.Revealed() || first.rev.Transition() != widget.RevealFade {
		t.Errorf("entering: revealed %v, transition %v", first.rev.Revealed(), first.rev.Transition())
	}
	_, _ = h.open(dmenu(), true, "b")
	h.loop.pump()
	if h.windows[0].closed {
		t.Fatal("the replaced surface closed before its exit played")
	}
	if first.rev.Revealed() || first.rev.Transition() != widget.RevealZoom {
		t.Errorf("exiting: revealed %v, transition %v; want the launcher's zoom exit", first.rev.Revealed(), first.rev.Transition())
	}
	if len(h.windows) != 2 {
		t.Fatalf("windows = %d, want the replacement mapped meanwhile", len(h.windows))
	}
	first.rev.Finish()
	if !h.windows[0].closed || h.windows[1].closed {
		t.Errorf("after the exit: first closed %v, second closed %v", h.windows[0].closed, h.windows[1].closed)
	}
}

func TestSuperKeyBindingsParse(t *testing.T) {
	table := compileKeys([]engine.Binding{{Action: "accept-entry", Keys: "Super+Return, Control+j"}})
	if len(table) != 2 {
		t.Fatalf("%d bindings, want the Super one too", len(table))
	}
	press, err := app.ParseAccel("Super+Return")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := lookupKey(table, press); !ok || b.action != table[0].action {
		t.Error("Super+Return finds no binding")
	}
	plain, _ := app.ParseAccel("Return")
	if _, ok := lookupKey(table, plain); ok {
		t.Error("plain Return matched the Super binding")
	}
}
