package powermenu

import (
	"errors"
	"testing"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
)

type fakeWindow struct{ closed bool }

func (w *fakeWindow) Close() { w.closed = true }

type harness struct {
	cfg     *config.Config
	m       *Menu
	opened  []app.LayerConfig
	windows []*fakeWindow
	ran     []string
	openErr error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{cfg: config.Defaults()}
	h.m = New(Deps{
		Config: func() *config.Config { return h.cfg },
		Open: func(cfg app.LayerConfig) (Window, error) {
			if h.openErr != nil {
				return nil, h.openErr
			}
			h.opened = append(h.opened, cfg)
			w := &fakeWindow{}
			h.windows = append(h.windows, w)
			return w, nil
		},
		Run:  func(cmd string) error { h.ran = append(h.ran, cmd); return nil },
		Font: face,
	})
	return h
}

// buttons are the menu's buttons in order.
func (h *harness) buttons(t *testing.T) []*widget.Button {
	t.Helper()
	var out []*widget.Button
	var walk func(w widget.Widget)
	walk = func(w widget.Widget) {
		if b, ok := w.(*widget.Button); ok {
			out = append(out, b)
			return
		}
		if c, ok := w.(interface{ Children() []widget.Widget }); ok {
			for _, k := range c.Children() {
				walk(k)
			}
		}
	}
	walk(h.opened[len(h.opened)-1].Root)
	return out
}

func TestShowMapsTheOverlay(t *testing.T) {
	h := newHarness(t)
	h.m.Show()
	if len(h.opened) != 1 {
		t.Fatalf("opened %d surfaces", len(h.opened))
	}
	cfg := h.opened[0]
	all := app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight
	if cfg.Layer != app.LayerOverlay || cfg.Anchor != all || cfg.Keyboard != app.KeyboardExclusive ||
		cfg.ExclusiveZone != -1 || cfg.Namespace != "wayle-power-menu" {
		t.Errorf("layer config = %+v", cfg)
	}
	if got := len(h.buttons(t)); got != 5 {
		t.Errorf("buttons = %d, want all five by default", got)
	}
	if !h.m.rev.Revealed() {
		t.Error("the menu is not entering")
	}
	h.m.Show() // already up
	if len(h.opened) != 1 {
		t.Error("a second Show while up mapped another surface")
	}
}

func TestHiddenActionsAreLeftOut(t *testing.T) {
	h := newHarness(t)
	h.cfg.Power.ShowReboot = false
	h.cfg.Power.ShowLogout = false
	h.m.Show()
	if got := len(h.buttons(t)); got != 3 {
		t.Errorf("buttons = %d, want the three still shown", got)
	}
}

func TestButtonRunsItsCommandAfterTheExit(t *testing.T) {
	h := newHarness(t)
	h.cfg.Power.SuspendCommand = "systemctl suspend"
	h.m.Show()
	h.m.rev.Finish()
	h.buttons(t)[2].OnClick() // lock, log out, suspend
	if len(h.ran) != 0 || h.windows[0].closed {
		t.Fatal("the command ran (or the menu closed) before the exit played")
	}
	if h.m.Open() {
		t.Error("the menu still reports open mid-exit")
	}
	h.m.rev.Finish()
	if !h.windows[0].closed || len(h.ran) != 1 || h.ran[0] != "systemctl suspend" {
		t.Errorf("after the exit: closed %v, ran %v", h.windows[0].closed, h.ran)
	}
	h.m.Show()
	if len(h.opened) != 2 {
		t.Error("the menu did not reopen after closing")
	}
}

func TestEscapeCancels(t *testing.T) {
	h := newHarness(t)
	h.cfg.Animations.Enabled = false
	h.m.Show()
	if !h.opened[0].KeyCapture(app.Accel{Sym: escape.Sym}) {
		t.Fatal("Escape was not consumed")
	}
	if !h.windows[0].closed || len(h.ran) != 0 {
		t.Errorf("Escape: closed %v, ran %v; want closed, nothing run", h.windows[0].closed, h.ran)
	}
	other, _ := app.ParseAccel("a")
	h.m.Show()
	if h.opened[1].KeyCapture(other) {
		t.Error("a plain key was consumed")
	}
	h.m.Cancel()
	h.m.Cancel() // closing already: no double close or run
	if len(h.ran) != 0 {
		t.Errorf("cancel ran %v", h.ran)
	}
}

func TestShowWhenTheOverlayCannotMap(t *testing.T) {
	h := newHarness(t)
	h.openErr = errors.New("no layer shell")
	h.m.Show()
	if h.m.Open() {
		t.Error("an unmapped menu reports open")
	}
	h.m.Cancel() // nothing to close
}
