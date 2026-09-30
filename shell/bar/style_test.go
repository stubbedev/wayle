package bar

import (
	"strings"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

func testStyle(t *testing.T, mutate func(b *config.Bar)) barStyle {
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

	if s.bg != palette.Surface {
		t.Errorf("bg = %#08x, want the bg-surface token at full opacity", s.bg)
	}
	if s.fg != palette.Fg {
		t.Errorf("fg = %#08x, want fg-default", s.fg)
	}
	if s.borders.any() {
		t.Errorf("borders = %+v, want none at the default location", s.borders)
	}
	if s.moduleGap != 8 { // 0.5 * 16 * 1.0
		t.Errorf("module-gap = %d, want 8", s.moduleGap)
	}
	if s.padding != 6 { // 0.35 * 16, rounded
		t.Errorf("padding = %d, want 6", s.padding)
	}
	if s.radius != 0 {
		t.Errorf("radius = %d, want 0 (rounding none)", s.radius)
	}
	if s.labelPx != 1.04*styling.RemBase {
		t.Errorf("label px = %v, want the 1.04rem base at multiplier 1.0", s.labelPx)
	}
}

func TestComputeStyleOpacityMixesToTransparent(t *testing.T) {
	if s := testStyle(t, func(b *config.Bar) { b.BackgroundOpacity = 0 }); s.bg>>24 != 0 {
		t.Errorf("opacity 0: bg = %#08x, want fully transparent", s.bg)
	}
	s := testStyle(t, func(b *config.Bar) { b.BackgroundOpacity = 50 })
	if s.bg>>24 != paletteSurfaceAlphaAt50() {
		t.Errorf("opacity 50: bg = %#08x, want the color-mix halved alpha", s.bg)
	}
}

func paletteSurfaceAlphaAt50() render.Color {
	p := styling.Default()
	return styling.ColorMix(p.Surface, transparentColor, 50) >> 24
}

func TestComputeStyleBordersFromLocation(t *testing.T) {
	s := testStyle(t, func(b *config.Bar) { b.BorderLocation = config.BorderTop })
	if !s.borders.any() || s.borders.top != 1 || s.borders.left != 0 {
		t.Errorf("top border: widths = %+v, want top-only at width 1", s.borders)
	}
	s = testStyle(t, func(b *config.Bar) {
		b.BorderLocation = config.BorderAll
		b.BorderWidth = 3
	})
	if s.borders != (borderWidths{left: 3, top: 3, right: 3, bottom: 3}) {
		t.Errorf("all border: widths = %+v, want every edge at 3", s.borders)
	}
}

func TestComputeStyleScalesSizes(t *testing.T) {
	s := testStyle(t, func(b *config.Bar) {
		b.Scale = 2
		b.ModuleGap = config.Size{Value: 0.5, Unit: config.SizeMultiplier}
		b.PaddingEnds = config.Size{Value: 8, Unit: config.SizePixels}
	})
	if s.moduleGap != 16 { // multipliers scale: 0.5 * 16 * 2
		t.Errorf("module-gap = %d, want 16", s.moduleGap)
	}
	if s.paddingEnds != 8 { // pixels ignore the scale
		t.Errorf("padding-ends = %d, want 8", s.paddingEnds)
	}
}

func TestComputeStyleLabelSize(t *testing.T) {
	if s := testStyle(t, nil); s.labelPx != 1.04*styling.RemBase {
		t.Errorf("default label px = %v", s.labelPx)
	}
	s := testStyle(t, func(b *config.Bar) {
		b.ButtonLabelSize = config.Size{Value: 14, Unit: config.SizePixels}
	})
	if s.labelPx != 14 {
		t.Errorf("pixel label size = %v, want 14 (literal)", s.labelPx)
	}
}

func TestComputeStyleGroupPaddingKeepsHistoricalFactor(t *testing.T) {
	// Multipliers carry styling.rs's 0.25 fine-tuning factor.
	s := testStyle(t, func(b *config.Bar) {
		b.ButtonGroupPadding = config.Size{Value: 1.0, Unit: config.SizeMultiplier}
	})
	if s.groupPadding != 4 { // 1.0 * 0.25 * 16
		t.Errorf("group padding = %d, want 4", s.groupPadding)
	}
	s = testStyle(t, func(b *config.Bar) {
		b.ButtonGroupPadding = config.Size{Value: 9, Unit: config.SizePixels}
	})
	if s.groupPadding != 9 {
		t.Errorf("group padding = %d, want 9 (pixels literal)", s.groupPadding)
	}
}

func TestStyleMarginsFollowLocation(t *testing.T) {
	cfg := config.Defaults()
	cfg.Bar.InsetEdge = config.Size{Value: 4, Unit: config.SizePixels}
	cfg.Bar.InsetEnds = config.Size{Value: 8, Unit: config.SizePixels}
	s := computeStyle(cfg, styling.Default())

	if got := s.margins(config.LocationTop); got != [4]int32{4, 8, 0, 8} {
		t.Errorf("top margins = %v, want edge 4 on top, ends 8 on the sides", got)
	}
	if got := s.margins(config.LocationBottom); got != [4]int32{0, 8, 4, 8} {
		t.Errorf("bottom margins = %v", got)
	}
	if got := s.margins(config.LocationLeft); got != [4]int32{8, 4, 8, 0} {
		t.Errorf("left margins = %v", got)
	}
	if got := s.margins(config.LocationRight); got != [4]int32{8, 0, 8, 4} {
		t.Errorf("right margins = %v", got)
	}
}

func TestBarStylesheetTargetsRootAndGroups(t *testing.T) {
	cfg := config.Defaults()
	style := computeStyle(cfg, styling.Default())
	sheet := barStylesheet(style)
	if !strings.Contains(sheet, ".bar {") || !strings.Contains(sheet, styling.HexRGBA(style.bg)) {
		t.Errorf("stylesheet misses the .bar background rule:\n%s", sheet)
	}
	if !strings.Contains(sheet, ".bar-group {") || !strings.Contains(sheet, styling.HexRGBA(style.groupBg)) {
		t.Errorf("stylesheet misses the .bar-group rule:\n%s", sheet)
	}
	if strings.Contains(sheet, "var(") {
		t.Error("stylesheet leaks CSS variables; gelm resolves concrete colors")
	}
}

// widgetConstraintsMax is the unconstrained-max helper shared by the
// painter tests.
func widgetConstraintsMax(w, h int) widget.Constraints {
	return widget.Constraints{Max: widget.Size{W: w, H: h}}
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

func TestBuildRootCarriesClassesAndBorder(t *testing.T) {
	cfg := config.Defaults()
	cfg.Bar.BorderLocation = config.BorderTop
	cfg.Bar.Layout = []config.BarLayout{{Monitor: "*", Center: []config.BarItem{{Module: "clock"}}}}
	ctx := newTestContext(t, cfg)
	root, err := buildRoot(ctx, cfg.Bar.Layout[0], "DP-1")
	if err != nil {
		t.Fatalf("buildRoot: %v", err)
	}
	if !widget.HasClass(root, "bar") || !widget.HasClass(root, "top") || !widget.HasClass(root, "DP-1") {
		t.Error("root misses the bar/location/connector classes")
	}
}

func TestBuildRootWithoutBorderIsTheContentItself(t *testing.T) {
	cfg := config.Defaults()
	cfg.Bar.Layout = []config.BarLayout{{Monitor: "*"}}
	ctx := newTestContext(t, cfg)
	root, err := buildRoot(ctx, cfg.Bar.Layout[0], "DP-1")
	if err != nil {
		t.Fatalf("buildRoot: %v", err)
	}
	if _, isOverlay := root.(*widget.Overlay); isOverlay {
		t.Error("borderless bar: root is an overlay, want the content box directly")
	}
}

func TestInsetMeasuresAndArranges(t *testing.T) {
	child := widget.NewBox(widget.Row, 0, 0)
	in := newInset(child, 2, 3, 4, 5)
	sz := in.Measure(widget.Constraints{Max: widget.Size{W: 100, H: 100}})
	if sz.W != 6 || sz.H != 8 {
		t.Fatalf("empty child 6x8 measured = %+v, want 6x8", sz)
	}
	in.Arrange(render.Rect{X: 10, Y: 20, W: 106, H: 108})
	bounds := child.Bounds()
	if bounds.X != 12 || bounds.Y != 23 || bounds.W != 100 || bounds.H != 100 {
		t.Errorf("child bounds = %+v, want inset by 2,3", bounds)
	}
}

func TestInsetClampsToConstraints(t *testing.T) {
	in := newInset(widget.NewBox(widget.Row, 0, 0), 2, 2, 2, 2)
	if sz := in.Measure(widget.Constraints{Max: widget.Size{W: 5, H: 5}}); sz.W != 4 || sz.H != 4 {
		t.Errorf("measure = %+v, want the 4x4 child-plus-insets", sz)
	}
	if sz := in.Measure(widget.Constraints{Max: widget.Size{W: 2, H: 2}}); sz.W != 2 || sz.H != 2 {
		t.Errorf("clamped measure = %+v, want 2x2 (insets never exceed the box)", sz)
	}
}

func TestBorderPainterPaintsOnlyTheEdges(t *testing.T) {
	const (
		w, h = 10, 6
	)
	data := make([]byte, render.Stride(w)*h)
	cv := render.New(data, render.Stride(w), w, h)
	cv.Clear(cv.Rect(), render.RGB(0, 0, 0))

	painter := newBorder(borderWidths{left: 2, top: 1}, render.RGB(0xff, 0, 0))
	painter.Arrange(render.Rect{X: 0, Y: 0, W: w, H: h})
	painter.Paint(cv)

	pixel := func(x, y int) render.Color {
		start := y*render.Stride(w) + x*4
		return render.ColorFromBytes(data[start : start+4])
	}
	red := render.RGB(0xff, 0, 0)
	black := render.RGB(0, 0, 0)
	for _, tc := range []struct {
		x, y int
		want render.Color
		note string
	}{
		{0, 0, red, "left edge"},
		{1, 3, red, "left edge mid-height"},
		{5, 0, red, "top edge"},
		{9, 3, black, "right side unpainted"},
		{5, 5, black, "bottom side unpainted"},
		{5, 3, black, "interior unpainted"},
	} {
		if got := pixel(tc.x, tc.y); got != tc.want {
			t.Errorf("pixel(%d,%d) [%s] = %#08x, want %#08x", tc.x, tc.y, tc.note, got, tc.want)
		}
	}
}

func TestButtonRadiusFollowsButtonRounding(t *testing.T) {
	s := testStyle(t, func(b *config.Bar) { b.ButtonRounding = config.RoundingLg })
	if want := styling.RoundingRadiusPx(config.RoundingLg, 1); s.buttonRadius != want {
		t.Errorf("button radius = %d, want button-rounding lg's %d", s.buttonRadius, want)
	}
	// The group rounding shapes the group, not its buttons.
	s = testStyle(t, func(b *config.Bar) { b.ButtonGroupRounding = config.RoundingFull })
	if want := styling.RoundingRadiusPx(config.RoundingSm, 1); s.buttonRadius != want {
		t.Errorf("button radius = %d, want the default sm %d with only the group rounding changed", s.buttonRadius, want)
	}
	if s.groupRadius != styling.RoundingRadiusPx(config.RoundingFull, 1) {
		t.Errorf("group radius = %d, want full", s.groupRadius)
	}
}
