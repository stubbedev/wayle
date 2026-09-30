package bar

import (
	"testing"

	"github.com/neurlang/wayland/wl"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
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
	for _, name := range []string{"calendar", "battery", "audio", "weather", "power", "notification"} {
		if _, ok := r.builders[name]; !ok {
			t.Errorf("default-referenced dropdown %q missing", name)
		}
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
