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

// panelBox gives a dropdown its panel: the card fills the width and the
// configured height (h < 0: the content's natural height, a
// content-sized dropdown). A page stack sized to its visible page (the
// network VPN editor's) grows the card past that by however much the
// visible page outgrows the room the stack gets, tweened by the stack's
// interpolate-size (dropdown_resize::animate_height).
//
// A mapped popup cannot resize mid-grab without flicker, so the panel
// reserves, up front, the room every stack's tallest page needs
// (dropdown_resize::measure, the worst stack, not the sum): the card
// moves inside that fixed surface, anchored at the bar's edge, and the
// transparent rest closes the dropdown on a click, as the Rust spacer's
// gesture does.
type panelBox struct {
	widget.Base
	w, h  int
	child widget.Widget
	// maxH caps the card's height (the monitor less the Rust registry's
	// scrolled-content margin); <= 0 takes the measure constraints.
	maxH int
	pop  popoverHandle
}

func newPanelBox(w, h int, child widget.Widget) *panelBox {
	return &panelBox{w: w, h: h, child: child}
}

// layout is the card's height and the surface's at width w: the base
// (the configured height, else the content's natural one) plus the
// worst stack shortfall - its visible page's floor, then its neediest
// page's, against the height the stack is given at the base.
func (p *panelBox) layout(w, maxH int) (card, surface int) {
	if p.maxH > 0 {
		maxH = min(maxH, p.maxH)
	}
	con := widget.Constraints{Min: widget.Size{W: w}, Max: widget.Size{W: w, H: maxH}}
	base := p.h
	if base < 0 {
		base = p.child.Measure(con).H
	}
	base = min(base, maxH)
	p.fit(w, base)
	grow, reserve := 0, 0
	walkStacks(p.child, func(s *widget.Stack) {
		// A page needs its natural height less what it gives up (a
		// scrolled list gives way): GTK's natural height, where a
		// scrolled window asks for none.
		given := s.Bounds().H
		grow = max(grow, s.Measure(con).H-s.Shrinkable()-given)
		reserve = max(reserve, s.PageFloor(con)-given)
	})
	return min(base+grow, maxH), min(base+reserve, maxH)
}

// fit lays the content out in a w x h card at the origin.
func (p *panelBox) fit(w, h int) {
	p.child.Measure(widget.Constraints{Min: widget.Size{W: w, H: h}, Max: widget.Size{W: w, H: h}})
	p.child.Arrange(render.Rect{W: w, H: h})
}

func (p *panelBox) Measure(con widget.Constraints) widget.Size {
	// The Rust registry sizes the surface to the card's natural width,
	// the configured width only a floor (the popover's width_request).
	nat := p.child.Measure(widget.Constraints{Max: widget.Size{W: con.Max.W}})
	w := max(p.w, nat.W)
	w = min(w, con.Max.W)
	_, surface := p.layout(w, con.Max.H)
	return widget.Size{W: w, H: surface}
}

// walkStacks visits every page stack under w (through each stack's
// visible page).
func walkStacks(w widget.Widget, visit func(*widget.Stack)) {
	if s, ok := w.(*widget.Stack); ok {
		visit(s)
	}
	if c, ok := w.(interface{ Children() []widget.Widget }); ok {
		for _, k := range c.Children() {
			walkStacks(k, visit)
		}
	}
}

func (p *panelBox) Arrange(r render.Rect) {
	p.ArrangeSelf(r)
	widget.SetParents(p, p.child)
	h, _ := p.layout(r.W, r.H)
	// The card sits at the surface's top unconditionally (the Rust
	// revealer's valign Start): the fixed surface's transparent rest
	// hangs below, and closes the dropdown on a click.
	p.child.Measure(widget.Constraints{Min: widget.Size{W: r.W, H: h}, Max: widget.Size{W: r.W, H: h}})
	p.child.Arrange(render.Rect{X: r.X, Y: r.Y, W: r.W, H: h})
}

func (p *panelBox) Paint(cv *render.Canvas) { widget.PaintChild(cv, p.child) }

// Children exposes the content to the tree walks.
func (p *panelBox) Children() []widget.Widget { return []widget.Widget{p.child} }

// HitTest is the card's content, else the transparent rest.
func (p *panelBox) HitTest(pt widget.Point) widget.Widget {
	if hit := p.child.HitTest(pt); hit != nil {
		return hit
	}
	return p.HitLeaf(p, pt)
}

// ClickAt on the transparent rest closes the dropdown.
func (p *panelBox) ClickAt(widget.Point) {
	if p.pop != nil {
		p.pop.Dismiss()
	}
}

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
	p.pop = h
	if d, ok := p.child.(dropdownAttacher); ok {
		d.attachPopover(h)
	}
}
