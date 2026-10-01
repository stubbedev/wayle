package bar

import (
	"testing"

	"github.com/neurlang/wayland/wl"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/reveal"
)

var _ = render.Color(0)

func TestDropdownBuildersCoverRegistryNames(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	r := newDropdownRegistry(nil, cfg, ctx.Font, ctx.Style, ctx)
	// Every name the click-action parser accepts as dropdown:<name> has
	// a builder that produces a tree.
	for _, name := range r.Names() {
		if build, ok := r.builders[name]; !ok || build == nil {
			t.Errorf("dropdown %q registered without a builder", name)
			continue
		}
		if got := r.builders[name](ctx); got == nil {
			t.Errorf("dropdown %q built nil content", name)
		}
	}
	// The names the shell's own defaults reference are present.
	for _, name := range []string{"calendar", "battery", "audio", "weather", "notification"} {
		if _, ok := r.builders[name]; !ok {
			t.Errorf("default-referenced dropdown %q missing", name)
		}
	}
	// Power is a menu overlay, not a dropdown (the Rust registry has none).
	if _, ok := r.builders["power"]; ok {
		t.Error("a power dropdown is registered")
	}
}

func TestDropdownOpenWithoutHostErrors(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	r := newDropdownRegistry(nil, cfg, ctx.Font, ctx.Style, ctx)
	// No host attached and no app: the open is an error, not a panic,
	// and the click path logs it.
	anchor := widget.NewLabel(ctx.Font, 12, "anchor", 0xFF000000)
	if err := r.open("DP-1", "calendar", anchor); err == nil {
		t.Fatal("hostless open: want an error")
	}
	// An unknown dropdown name errors too.
	r.attachHost("DP-1", fakeHost{})
	if err := r.open("DP-1", "nope", anchor); err == nil {
		t.Fatal("unknown dropdown: want an error")
	}
}

func TestDropdownToggleSemantics(t *testing.T) {
	// The re-click dismiss path runs before any host lookup: with the
	// popover open, the second click dismisses instead of re-opening.
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	r := newDropdownRegistry(nil, cfg, ctx.Font, ctx.Style, ctx)
	// Without an app the popover was never opened, so openPop stays
	// empty; exercise the map bookkeeping directly.
	r.mu.Lock()
	r.openPop["DP-1"] = nil
	r.mu.Unlock()
	// nil popover: the toggle branch does not fire (nil != non-nil).
	anchor := widget.NewLabel(ctx.Font, 12, "anchor", 0xFF000000)
	if err := r.open("DP-1", "calendar", anchor); err == nil {
		t.Fatal("hostless open: want an error")
	}
}

// fakeHost satisfies app.Host for the registry's host lookups.
type fakeHost struct{}

func (fakeHost) EnsureUsable() error      { return nil }
func (fakeHost) Closed() bool             { return false }
func (fakeHost) Size() (int, int)         { return 800, 32 }
func (fakeHost) HostSurface() *wl.Surface { return nil }

// Dropdown builders get the full style even when the bar's modules run
// with fg left to the stylesheet (zero): a popover has no stylesheet, so
// a zero fg would paint every default-colored dropdown label
// transparent.
func TestDropdownBuildersGetTheFullStyle(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	full := *ctx.Style
	module := full
	module.fg = 0
	ctx.Style = &module
	r := newDropdownRegistry(nil, cfg, ctx.Font, &full, ctx)
	if r.ctx.Style.fg == 0 || r.ctx.Style != &full {
		t.Fatalf("builders see fg %08x, want the full style's %08x", uint32(r.ctx.Style.fg), uint32(full.fg))
	}
	v := calendarDropdown(r.ctx).(*calendarView)
	if v.hours == nil {
		t.Fatal("no hours label")
	}
	if ctx.Style.fg != 0 {
		t.Error("the module context's style was changed")
	}
}

func TestDropdownGravityOpensAwayFromTheEdge(t *testing.T) {
	for loc, want := range map[config.Location]app.Gravity{
		config.LocationTop: app.GravityBottom, config.LocationBottom: app.GravityTop,
		config.LocationLeft: app.GravityRight, config.LocationRight: app.GravityLeft,
	} {
		if got := dropdownGravity(loc); got != want {
			t.Errorf("dropdownGravity(%s) = %v, want %v", loc, got, want)
		}
	}
}

// fakePopover records what dropdown content asked of its popover.
type fakePopover struct {
	dismissed int
	focused   widget.Widget
}

func (f *fakePopover) Dismiss()                 { f.dismissed++ }
func (f *fakePopover) SetFocus(w widget.Widget) { f.focused = w }

func TestDropdownScrollIsVerticalOnly(t *testing.T) {
	s := dropdownScroll(widget.NewBox(widget.Column, 0, 0), "picker-body")
	if !s.VerticalOnly || !s.HasClass("picker-body") {
		t.Error("a dropdown scroll must scroll vertically only, with its class")
	}
	if bare := dropdownScroll(widget.NewBox(widget.Column, 0, 0), ""); len(bare.Classes()) != 0 {
		t.Errorf("an empty class was added: %v", bare.Classes())
	}
}

type fakeDismisser struct{ dismissed int }

func (f *fakeDismisser) Dismiss() { f.dismissed++ }

// TestDropdownDismissPlaysTheExit pins animate_out: a programmatic
// close keeps the popover through the card's exit and dismisses it
// when that lands; disabled animations dismiss at once.
func TestDropdownDismissPlaysTheExit(t *testing.T) {
	anims := config.DefaultsAnimations()
	rev := widget.NewRevealer(widget.NewBox(widget.Row, 0, 0))
	reveal.Show(rev, anims, config.AnimDropdown)
	rev.Finish()
	pop := &fakeDismisser{}
	dismissAnimated(pop, rev, anims)
	if pop.dismissed != 0 || rev.Revealed() {
		t.Fatalf("mid-exit: %d dismisses, revealed %v", pop.dismissed, rev.Revealed())
	}
	rev.Finish()
	if pop.dismissed != 1 {
		t.Errorf("after the exit: %d dismisses, want 1", pop.dismissed)
	}

	anims.Enabled = false
	rev = widget.NewRevealer(widget.NewBox(widget.Row, 0, 0))
	reveal.Show(rev, anims, config.AnimDropdown)
	pop = &fakeDismisser{}
	dismissAnimated(pop, rev, anims)
	if pop.dismissed != 1 {
		t.Errorf("animations off: %d dismisses, want 1 at once", pop.dismissed)
	}
	dismissAnimated(pop, nil, anims)
	if pop.dismissed != 2 {
		t.Error("a popover without a revealer was not dismissed")
	}
}

func TestDropdownGenieEdgeIsTheBars(t *testing.T) {
	for loc, want := range map[config.Location]widget.Edge{
		config.LocationTop: widget.EdgeTop, config.LocationBottom: widget.EdgeBottom,
		config.LocationLeft: widget.EdgeLeft, config.LocationRight: widget.EdgeRight,
	} {
		if got := dropdownGenieEdge(loc); got != want {
			t.Errorf("%s: %v, want %v", loc, got, want)
		}
	}
}
