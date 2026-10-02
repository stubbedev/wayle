package bar

import (
	"math"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// dropdownGeometry is a dropdown's built-in size, each Rust dropdown's
// BASE_WIDTH and BASE_HEIGHT at scale 1: a zero height grows to fit the
// content. size picks its [dropdowns.<name>] override; nil for a
// dropdown with none (the recorder).
type dropdownGeometry struct {
	width, height float64
	size          func(*config.DropdownsConfig) config.DropdownSize
}

// dropdownGeometries covers every sized dropdown; one absent here (the
// power menu) keeps its natural size.
var dropdownGeometries = map[string]dropdownGeometry{
	"audio":        {382, 512, func(d *config.DropdownsConfig) config.DropdownSize { return d.Audio }},
	"volume":       {382, 512, func(d *config.DropdownsConfig) config.DropdownSize { return d.Audio }},
	"microphone":   {382, 512, func(d *config.DropdownsConfig) config.DropdownSize { return d.Audio }},
	"battery":      {382, 312, func(d *config.DropdownsConfig) config.DropdownSize { return d.Battery }},
	"bluetooth":    {382, 512, func(d *config.DropdownsConfig) config.DropdownSize { return d.Bluetooth }},
	"brightness":   {320, 0, func(d *config.DropdownsConfig) config.DropdownSize { return d.Brightness }},
	"calendar":     {340, 0, func(d *config.DropdownsConfig) config.DropdownSize { return d.Calendar }},
	"dashboard":    {380, 0, func(d *config.DropdownsConfig) config.DropdownSize { return d.Dashboard }},
	"mail":         {300, 0, func(d *config.DropdownsConfig) config.DropdownSize { return d.Mail }},
	"media":        {380, 410, func(d *config.DropdownsConfig) config.DropdownSize { return d.Media }},
	"network":      {382, 512, func(d *config.DropdownsConfig) config.DropdownSize { return d.Network }},
	"notification": {425, 725, func(d *config.DropdownsConfig) config.DropdownSize { return d.Notification }},
	"recorder":     {360, 0, nil},
	"treeman":      {440, 460, func(d *config.DropdownsConfig) config.DropdownSize { return d.Treeman }},
	"weather":      {395, 695, func(d *config.DropdownsConfig) config.DropdownSize { return d.Weather }},
}

// dropdownDims resolves a dropdown's panel size in logical pixels
// (resolve_dimension and resolve_content_height): an override scales
// the base or sets pixels, no override is the base at the global
// scale. A content-height dropdown takes only a pixel height
// override; otherwise its height is -1, natural. ok is false for a
// dropdown with no geometry.
func dropdownDims(name string, cfg *config.Config) (w, h int, ok bool) {
	g, ok := dropdownGeometries[name]
	if !ok {
		return 0, 0, false
	}
	scale := float64(cfg.Styling.Scale)
	if scale <= 0 {
		scale = 1
	}
	var size config.DropdownSize
	if g.size != nil {
		size = g.size(&cfg.Dropdowns)
	}
	w = resolveDimension(size.Width, g.width, scale)
	switch {
	case g.height > 0:
		h = resolveDimension(size.Height, g.height, scale)
	case size.Height != nil && size.Height.Unit == config.SizePixels:
		h = int(math.Round(float64(size.Height.Value)))
	default:
		h = -1
	}
	return w, h, true
}

func resolveDimension(override *config.Size, base, scale float64) int {
	if override != nil {
		return int(math.Round(override.ResolvePx(base, scale)))
	}
	return int(math.Round(base * scale))
}

// panelBox gives a dropdown its panel size: the child fills the width,
// and the height when one is set (h < 0 is the child's natural height,
// a content-sized dropdown).
type panelBox struct {
	widget.Base
	w, h  int
	child widget.Widget
}

func newPanelBox(w, h int, child widget.Widget) *panelBox {
	return &panelBox{w: w, h: h, child: child}
}

func (p *panelBox) Measure(con widget.Constraints) widget.Size {
	w := min(p.w, con.Max.W)
	if p.h >= 0 {
		return widget.Size{W: w, H: min(p.h, con.Max.H)}
	}
	sz := p.child.Measure(widget.Constraints{Min: widget.Size{W: w}, Max: widget.Size{W: w, H: con.Max.H}})
	return widget.Size{W: w, H: sz.H}
}

func (p *panelBox) Arrange(r render.Rect) {
	p.ArrangeSelf(r)
	widget.SetParents(p, p.child)
	p.child.Measure(widget.Constraints{Min: widget.Size{W: r.W, H: r.H}, Max: widget.Size{W: r.W, H: r.H}})
	p.child.Arrange(r)
}

func (p *panelBox) Paint(cv *render.Canvas) { widget.PaintChild(cv, p.child) }

// Children exposes the content to the tree walks.
func (p *panelBox) Children() []widget.Widget { return []widget.Widget{p.child} }

func (p *panelBox) HitTest(pt widget.Point) widget.Widget { return p.child.HitTest(pt) }

// dropdownCloser forwards to the content, so a sized dropdown still
// stops following its service when the popover closes.
func (p *panelBox) dropdownClosed() {
	if c, ok := p.child.(dropdownCloser); ok {
		c.dropdownClosed()
	}
}

// attachPopover forwards the popover handle to content that acts on
// its popover.
func (p *panelBox) attachPopover(h popoverHandle) {
	if d, ok := p.child.(dropdownAttacher); ok {
		d.attachPopover(h)
	}
}
