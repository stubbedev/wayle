package bar

import (
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// The dropdown panels paint from the stylesheet: a migrated label
// carries no programmatic color and the cascade inks it, the flat
// panel buttons carry no Go hover fills, and the panel icons follow
// the cascade color instead of a pinned tint.
func TestDropdownPanelsPaintFromTheStylesheet(t *testing.T) {
	cfg := config.Defaults()
	ctx := styledContext(t, cfg)
	v := audioDropdown(ctx).(*audioView)
	ctx.Theme.Attach(v)
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: 400, H: 600}})
	v.Arrange(render.Rect{W: sz.W, H: 600})

	if got := v.output.name.Color(); got != 0 {
		t.Errorf("the device name carries a programmatic color %#08x", uint32(got))
	}
	if v.output.mute.BgHover != 0 || v.output.mute.BgPressed != 0 {
		t.Error("the mute button carries Go hover fills")
	}
	if got := v.output.icon.Tint(); got != 0 {
		t.Errorf("the device icon carries a pinned tint %#08x", uint32(got))
	}

	// The sheet demonstrably drives the ink: the same uncolored label
	// paints differently under the panel's sheet (the cascade's
	// fg-default) than alone (the widget theme's fallback).
	name := v.output.name
	name.SetText("Speakers")
	withSheet := paintLabel(name)
	bare := widget.NewLabel(ctx.Font, ctx.Style.labelPx, "Speakers", 0)
	withoutSheet := paintLabel(bare)
	if string(withSheet) == string(withoutSheet) {
		t.Error("the stylesheet did not reach the panel tree: the label painted the fallback ink")
	}
}

// paintLabel paints one label into a fresh buffer.
func paintLabel(l *widget.Label) []byte {
	sz := l.Measure(widget.Constraints{Max: widget.Size{W: 200, H: 40}})
	l.Arrange(render.Rect{W: sz.W + 8, H: 24})
	data := make([]byte, render.Stride(200)*40)
	cv := render.NewScaled(data, render.Stride(200), 200, 40, 1, 1)
	cv.Clear(cv.Rect(), 0)
	l.Paint(cv)
	return data
}

// countColor counts the pixels col accepts.
func countColor(data []byte, col func(render.Color) bool) int {
	n := 0
	for i := 0; i+3 < len(data); i += 4 {
		if col(render.ColorFromBytes(data[i:])) {
			n++
		}
	}
	return n
}

// The battery gauge is a levelbar: the trough and the filled block
// paint from the levelbar.battery-gauge rules, the variant class
// recolors the block, and no Go color survives.
func TestBatteryGaugePaintsFromTheLevelbarRules(t *testing.T) {
	ctx := styledContext(t, config.Defaults())
	v := batteryDropdown(ctx).(*batteryView)
	defer v.dropdownClosed()
	// The tree pass first: the gauge's element resolves on parenting.
	sz0 := v.Measure(widget.Constraints{Max: widget.Size{W: 400, H: 600}})
	v.Arrange(render.Rect{W: sz0.W, H: 600})
	ctx.Theme.Attach(v)
	restore := widget.SetAnimationsInstant(true)
	t.Cleanup(restore)
	v.gauge.SetValue(0.5)
	v.gauge.RemoveClass(batteryVariants...)
	v.gauge.AddClass("good")
	sz := v.gauge.Measure(widget.Constraints{Max: widget.Size{W: 200, H: 40}})
	v.gauge.Arrange(render.Rect{X: 0, Y: 0, W: 200, H: sz.H})

	palette := ctx.Theme.RenderPalette()
	match := func(want render.Color) func(render.Color) bool {
		return func(c render.Color) bool {
			return max(int(c.R())-int(want.R()), int(want.R())-int(c.R())) < 24 &&
				max(int(c.G())-int(want.G()), int(want.G())-int(c.G())) < 24
		}
	}
	paint := func() []byte {
		data := make([]byte, render.Stride(200)*40)
		cv := render.NewScaled(data, render.Stride(200), 200, 40, 1, 1)
		cv.Clear(cv.Rect(), 0)
		v.gauge.Paint(cv)
		return data
	}
	// The good block: the success color, half the bar.
	data := paint()
	a, b := spanOf(data, sz.H, match(palette.Green))
	if a < 0 || (b-a) < 90 || a > 10 {
		t.Errorf("the good block spans %d..%d, want the left half", a, b)
	}
	if n := countColor(data, match(palette.Red)); n != 0 {
		t.Error("the good gauge shows the error color")
	}
	// The crit class recolors it without a Go color.
	v.gauge.RemoveClass("good")
	v.gauge.AddClass("crit")
	a, b = spanOf(paint(), sz.H, match(palette.Red))
	if a < 0 || (b-a) < 90 {
		t.Errorf("the crit block spans %d..%d, want the left half", a, b)
	}
	if v.gauge.Value() != 0.5 {
		t.Errorf("the gauge value moved to %v", v.gauge.Value())
	}
}

// The dashboard rings paint from their cascade: the canvas color
// follows the variant class, and the stroke is the rule's border
// width — no threshold tint survives.
func TestDashboardRingPaintsFromTheCascade(t *testing.T) {
	ctx := styledContext(t, config.Defaults())
	r, overlay := newProgressRing(48, "lg", ctx.Font, 12)
	ctx.Theme.Attach(overlay)
	r.set(0.75, "75%", "success")
	sz := overlay.Measure(widget.Constraints{Max: widget.Size{W: 100, H: 100}})
	overlay.Arrange(render.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})

	if got := widget.CascadeColor(r); got == 0 {
		t.Error("the canvas cascade has no color: the .progress-ring-canvas rule did not reach it")
	}
	if stroke := r.stroke(); stroke <= 0 {
		t.Fatalf("the ring stroke = %d, want the rule's border width", stroke)
	}
	paintRing := func() []byte {
		data := make([]byte, render.Stride(100)*100)
		cv := render.NewScaled(data, render.Stride(100), 100, 100, 1, 1)
		cv.Clear(cv.Rect(), 0)
		overlay.Paint(cv)
		return data
	}
	if n := countColor(paintRing(), func(c render.Color) bool { return c.G() > 150 && c.R() < 150 }); n == 0 {
		t.Error("the success ring painted no green")
	}
	r.set(0.75, "75%", "error")
	if n := countColor(paintRing(), func(c render.Color) bool { return c.R() > 150 && c.G() < 150 }); n == 0 {
		t.Error("the error ring painted no red")
	}
}

// spanOf walks one painted row set for the block's extent.
func spanOf(data []byte, h int, want func(render.Color) bool) (minX, maxX int) {
	minX, maxX = 1<<30, -1
	for y := range h {
		for x := range 200 {
			if want(render.ColorFromBytes(data[(y*200+x)*4:])) {
				minX = min(minX, x)
				maxX = max(maxX, x)
			}
		}
	}
	return minX, maxX
}
