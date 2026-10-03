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
