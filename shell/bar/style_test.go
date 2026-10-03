package bar

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/apptheme"
	"github.com/stubbedev/wayle/styling"
)

func testStyle(t *testing.T, mutate func(b *config.BarConfig)) barStyle {
	t.Helper()
	cfg := config.Defaults()
	if mutate != nil {
		mutate(&cfg.Bar)
	}
	return computeStyle(cfg, styling.Default())
}

func TestComputeStyleDefaults(t *testing.T) {
	s := testStyle(t, nil)
	palette := styling.Default()
	if s.fg != palette.Fg {
		t.Errorf("fg = %#08x, want fg-default", s.fg)
	}
	if s.moduleGap != 8 { // 0.5 * 16 * 1.0
		t.Errorf("module-gap = %d, want 8", s.moduleGap)
	}
	if s.labelPx != 1.04*styling.RemBase {
		t.Errorf("label px = %v, want the 1.04rem base at multiplier 1.0", s.labelPx)
	}
	if px := testStyle(t, func(b *config.BarConfig) { b.ButtonLabelSize = config.Size{Value: 20, Unit: config.SizePixels} }).labelPx; px != 20 {
		t.Errorf("pixel label size = %v, want literal 20", px)
	}
}

func widgetConstraintsMax(w, h int) widget.Constraints {
	return widget.Constraints{Max: widget.Size{W: w, H: h}}
}

// isolateConfigDir points the config dir at a temp dir, so the theme
// never reads the developer's own styles.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "wayle")
}

func newTestContext(t *testing.T, cfg *config.Config) ModuleContext {
	t.Helper()
	style := computeStyle(cfg, styling.Default())
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatalf("test font: %v", err)
	}
	return ModuleContext{Config: cfg, Font: face, Style: &style}
}

// styledContext is newTestContext with the bar stylesheet attached, as
// RunWith wires it.
func styledContext(t *testing.T, cfg *config.Config) ModuleContext {
	t.Helper()
	isolateConfigDir(t)
	ctx := newTestContext(t, cfg)
	ctx.Theme = apptheme.New(cfg)
	return ctx
}

// paintBar builds, lays out, and paints one bar root at width w,
// returning the pixels and the measured height.
func paintBar(t *testing.T, ctx ModuleContext, layout config.BarLayout, w int) ([]byte, int, widget.Widget) {
	t.Helper()
	root := buildRoot(ctx, layout, "DP-1")
	h := measureThickness(root, widget.Size{W: w}, false)
	root.Measure(widgetConstraintsMax(w, h))
	root.Arrange(render.Rect{W: w, H: h})
	widget.CollectDamage(root)
	root.Measure(widgetConstraintsMax(w, h))
	root.Arrange(render.Rect{W: w, H: h})
	data := make([]byte, render.Stride(w)*max(h, 1))
	root.Paint(render.New(data, render.Stride(w), w, max(h, 1)))
	return data, h, root
}

func pixelAt(data []byte, w, x, y int) render.Color {
	o := y*render.Stride(w) + x*4
	return render.ColorFromBytes(data[o : o+4])
}

func TestBarRootCarriesTheRustTreeAndClasses(t *testing.T) {
	cfg := config.Defaults()
	layout := config.BarLayout{Monitor: "*", Center: []config.BarItem{{Module: "clock", Class: "mine"}}}
	ctx := newTestContext(t, cfg)
	root := buildRoot(ctx, layout, "DP-1")
	for _, c := range []string{"bar", "top", "DP-1"} {
		if !widget.HasClass(root, c) {
			t.Errorf("root misses %q", c)
		}
	}
	if widget.HasClass(root, "floating") {
		t.Error("an inset-free bar is floating")
	}
	box := root.(*widget.Box)
	if box.Element() != "window" {
		t.Errorf("root element = %q, want window", box.Element())
	}
	if !strings.Contains(box.InlineStyle(), "--bar-padding-px: 6") {
		t.Errorf("root inline vars = %q, want the build_css block", box.InlineStyle())
	}
	center := box.Children()[0].(*widget.Box)
	var sections []*widget.Box
	for _, k := range center.Children() {
		if widget.HasClass(k, "bar-section") {
			sections = append(sections, k.(*widget.Box))
		}
	}
	if len(sections) != 3 || !widget.HasClass(sections[0], "bar-left") ||
		!widget.HasClass(sections[1], "bar-center") || !widget.HasClass(sections[2], "bar-right") {
		t.Fatalf("sections = %d, want left/center/right always present", len(sections))
	}
	items := sections[1].Children()
	if len(items) != 1 || !widget.HasClass(items[0], "bar-item") {
		t.Fatalf("center items = %v, want one bar-item", items)
	}
	// The module wrapper carries module + the layout instance class;
	// the bar button sits inside it.
	wrap, ok := items[0].(*widget.Box).Children()[0].(*widget.Box)
	if !ok {
		t.Fatalf("bar-item child = %T, want the module wrapper", items[0].(*widget.Box).Children()[0])
	}
	btn, ok := wrap.Children()[0].(*barButton)
	if !ok {
		t.Fatalf("wrapper child = %T, want the bar button", wrap.Children()[0])
	}
	for _, c := range []string{"bar-button", cfg.Bar.ButtonVariant.CSSClass()} {
		if !btn.HasClass(c) {
			t.Errorf("bar button misses %q (classes %v)", c, btn.Classes())
		}
	}
	for _, c := range []string{"module", "mine"} {
		if !wrap.HasClass(c) {
			t.Errorf("module wrapper misses %q (classes %v)", c, wrap.Classes())
		}
	}

	cfg.Bar.InsetEdge = config.Size{Value: 4, Unit: config.SizePixels}
	root = buildRoot(newTestContext(t, cfg), layout, "DP-1")
	if !widget.HasClass(root, "floating") {
		t.Error("an inset bar is not floating")
	}
}

func TestGroupsAreBarItemsWithModules(t *testing.T) {
	cfg := config.Defaults()
	layout := config.BarLayout{Monitor: "*", Left: []config.BarItem{{Group: &config.BarGroup{
		Name: "g1", Modules: []config.BarItem{{Module: "clock"}, {Module: "clock"}},
	}}}}
	root := buildRoot(newTestContext(t, cfg), layout, "DP-1")
	left := root.(*widget.Box).Children()[0].(*widget.Box).Children()[0].(*widget.Box)
	group := left.Children()[0].(*widget.Box)
	if !group.HasClass("bar-item") || !group.HasClass("bar-group") || group.ID() != "g1" {
		t.Errorf("group box classes %v id %q", group.Classes(), group.ID())
	}
	if n := len(group.Children()); n != 2 {
		t.Fatalf("group children = %d, want 2", n)
	}
	for _, k := range group.Children() {
		if !widget.HasClass(k, "module") {
			t.Error("a grouped module lacks the module class")
		}
	}
}

func TestBarPaintsFromTheRustStylesheet(t *testing.T) {
	cfg := config.Defaults()
	cfg.Bar.BorderLocation = config.BorderBottom
	cfg.Bar.BorderWidth = 2
	cfg.Bar.BorderColor = mustToken(config.TokenRed)
	ctx := styledContext(t, cfg)
	palette := ctx.Theme.RenderPalette()
	data, h, _ := paintBar(t, ctx, config.BarLayout{Monitor: "*"}, 200)
	if h <= 2 {
		t.Fatalf("bar height = %d", h)
	}
	// .bar: background-color from --bar-bg (bg-surface), the bottom
	// border from --bar-border-bottom and --bar-border-color.
	if got := pixelAt(data, 200, 100, 1); got != palette.Surface {
		t.Errorf("bar background = %#08x, want bg-surface %#08x", uint32(got), uint32(palette.Surface))
	}
	if got := pixelAt(data, 200, 100, h-1); got != palette.Red {
		t.Errorf("bottom border = %#08x, want red %#08x", uint32(got), uint32(palette.Red))
	}
	if got := pixelAt(data, 200, 100, 0); got == palette.Red {
		t.Error("the top edge carries the bottom-only border")
	}

	// background-opacity 0: color-mix to transparent.
	cfg.Bar.BackgroundOpacity = 0
	cfg.Bar.BorderLocation = config.BorderNone
	data, _, _ = paintBar(t, styledContext(t, cfg), config.BarLayout{Monitor: "*"}, 200)
	if got := pixelAt(data, 200, 100, 1); got.A() != 0 {
		t.Errorf("opacity 0 bar = %#08x, want transparent", uint32(got))
	}
}

func TestBarInsetsAreWindowMargins(t *testing.T) {
	cfg := config.Defaults()
	cfg.Bar.InsetEdge = config.Size{Value: 10, Unit: config.SizePixels}
	ctx := styledContext(t, cfg)
	palette := ctx.Theme.RenderPalette()
	_, flatH, _ := paintBar(t, styledContext(t, config.Defaults()), config.BarLayout{Monitor: "*"}, 200)
	data, h, _ := paintBar(t, ctx, config.BarLayout{Monitor: "*"}, 200)
	if h != flatH+10 {
		t.Errorf("inset bar height = %d, want the flat %d plus the 10px edge margin", h, flatH)
	}
	if got := pixelAt(data, 200, 100, 5); got.A() != 0 {
		t.Errorf("inside the edge margin = %#08x, want transparent", uint32(got))
	}
	if got := pixelAt(data, 200, 100, 11); got != palette.Surface {
		t.Errorf("below the margin = %#08x, want the bar", uint32(got))
	}
	lc := layerConfigFor(ctx, config.BarLayout{Monitor: "*"}, "DP-1", widget.Size{W: 200, H: 100})
	if lc.Margin != (app.Margins{}) || lc.Height != uint32(h) {
		t.Errorf("layer margins %+v height %d, want none and the full %d", lc.Margin, lc.Height, h)
	}
}

// A side bar is anchored top and bottom, so the compositor stretches
// its height and the bar sizes only its width; a top bar the other way
// round. Setting the stretched axis, or leaving the free axis zero, is
// a layer-shell protocol error.
func TestLayerConfigSizesOnlyTheFreeAxis(t *testing.T) {
	out := widget.Size{W: 1280, H: 800}
	for _, tc := range []struct {
		loc      config.Location
		vertical bool
	}{
		{config.LocationTop, false},
		{config.LocationBottom, false},
		{config.LocationLeft, true},
		{config.LocationRight, true},
	} {
		cfg := config.Defaults()
		cfg.Bar.Location = tc.loc
		layout := config.BarLayout{Monitor: "*", Center: []config.BarItem{{Module: "clock"}}}
		lc := layerConfigFor(styledContext(t, cfg), layout, "DP-1", out)
		thick, stretched := lc.Height, lc.Width
		if tc.vertical {
			thick, stretched = lc.Width, lc.Height
		}
		if thick == 0 || thick >= uint32(min(out.W, out.H)) {
			t.Errorf("%s: free-axis size %d, want the content's thickness", tc.loc, thick)
		}
		if stretched != 0 {
			t.Errorf("%s: stretched-axis size %d, want 0 (the compositor fills it)", tc.loc, stretched)
		}
		if lc.ExclusiveZone != int32(thick) {
			t.Errorf("%s: exclusive zone %d, want the thickness %d", tc.loc, lc.ExclusiveZone, thick)
		}
	}
}
