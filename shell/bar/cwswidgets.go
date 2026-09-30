package bar

import (
	"math"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// cwsContainer is the `.workspaces.<kind>` box: the buttons in a row
// (a column on a side bar), over the container background at the
// button bg opacity, with the border-show strips on top.
type cwsContainer struct {
	widget.Base
	m       *cwsView
	box     *widget.Box
	bg      render.Color
	radius  int
	borders borderWidths
	border  render.Color
}

func newCwsContainer(m *cwsView) *cwsContainer {
	axis := widget.Row
	if m.vertical() {
		axis = widget.Column
	}
	c := &cwsContainer{m: m, box: widget.NewBox(axis, 0, 0)}
	c.AddClass("workspaces")
	if m.kind != "" {
		c.AddClass(m.kind)
	}
	palette := m.palette()
	if base, ok := styling.ResolveColor(m.cfg.ContainerBgColor, palette); ok {
		opacity := 100
		if m.ctx.Config != nil {
			opacity = m.ctx.Config.Bar.ButtonBGOpacity
		}
		c.bg = styling.ColorMix(base, transparentColor, opacity)
	}
	// border-radius: calc(var(--bar-button-rounding-element) * 1.2)
	c.radius = int(math.Round(float64(m.buttonRadius()) * 1.2))
	if m.cfg.BorderShow && m.ctx.Config != nil {
		loc := m.ctx.Config.Bar.ButtonBorderLocation
		c.borders = borderWidths{}.fromLocation(loc, m.ctx.Config.Bar.ButtonBorderWidth)
		if loc != config.BorderNone {
			c.AddClass("border-" + string(loc))
		}
		c.border, _ = styling.ResolveColor(m.cfg.BorderColor, palette)
	}
	return c
}

// setButtons swaps in one button per model.
func (c *cwsContainer) setButtons(models []cwsButtonModel) {
	c.box.Clear()
	for _, model := range models {
		c.box.Append(newCwsButton(c.m, model), false)
	}
	c.box.InvalidateLayout()
	c.InvalidateLayout()
}

// buttons exposes the current buttons (tests, click routing).
func (c *cwsContainer) buttons() []*cwsButton {
	kids := c.box.Children()
	out := make([]*cwsButton, 0, len(kids))
	for _, k := range kids {
		if b, ok := k.(*cwsButton); ok {
			out = append(out, b)
		}
	}
	return out
}

func (c *cwsContainer) Measure(con widget.Constraints) widget.Size { return c.box.Measure(con) }

func (c *cwsContainer) Arrange(r render.Rect) {
	c.Base.Arrange(r)
	c.box.Arrange(r)
	widget.SetParents(c, c.box)
}

func (c *cwsContainer) ArrangeRoot(r render.Rect) { c.Arrange(r) }

func (c *cwsContainer) Paint(cv *render.Canvas) {
	r := c.Bounds()
	if c.bg != 0 {
		cv.RoundedRect(r, c.radius, c.bg)
	}
	c.box.Paint(cv)
	if c.borders.any() {
		// The border-show edges, rounded with the container.
		cv.RoundedBorder(r, render.UniformCorners(c.radius), c.borders.insets(), c.border)
	}
}

func (c *cwsContainer) HitTest(p widget.Point) widget.Widget {
	if hit := c.box.HitTest(p); hit != nil {
		return hit
	}
	return c.HitLeaf(c, p)
}

func (c *cwsContainer) Children() []widget.Widget { return []widget.Widget{c.box} }

// The pixel resolvers the widgets share.

func (m *cwsView) palette() *styling.Palette {
	if m.ctx.Style != nil && m.ctx.Style.palette != nil {
		return m.ctx.Style.palette
	}
	return styling.Default()
}

// buttonRadius is --bar-button-rounding-element.
func (m *cwsView) buttonRadius() int {
	if m.ctx.Config == nil {
		return styling.RoundingRadiusPx(config.RoundingSm, 1)
	}
	return styling.RoundingRadiusPx(m.ctx.Config.Bar.ButtonRounding, m.scale())
}

// fg is the inherited foreground a rule-less child paints with.
func (m *cwsView) fg() render.Color {
	if m.ctx.Style != nil {
		return m.ctx.Style.fg
	}
	return m.palette().Fg
}

func (m *cwsView) resolve(cv config.ColorValue) render.Color {
	color, _ := styling.ResolveColor(cv, m.palette())
	return color
}

// cwsColors is one button's resolved _workspaces.scss cascade.
type cwsColors struct {
	bg, hoverBg render.Color
	underline   render.Color
	label       render.Color // .workspace-label, .workspace-divider
	icon        render.Color // .workspace-icon (app icons; sway/niri/mango mapped icons)
	custom      render.Color // .workspace-custom-icon (hyprland's mapped icon)
	emptyIcon   render.Color // .workspace-icon-empty
	opacity     float64
}

// cwsResolveColors walks the _workspaces.scss rules for one button's
// classes: the override color stands in for every state color, the
// indicator decides between a filled and an underlined active state,
// and urgency without urgent-application halves the opacity.
func (m *cwsView) cwsResolveColors(model cwsButtonModel) cwsColors {
	override, hasOverride := cwsOverrideColor(model.classes, m.cfg.WorkspaceMap)
	pick := func(cv config.ColorValue) render.Color {
		if hasOverride {
			return m.resolve(override)
		}
		return m.resolve(cv)
	}
	active := hasClass(model.classes, "active")
	fg := m.fg()
	out := cwsColors{label: fg, icon: fg, custom: fg, emptyIcon: fg, opacity: 1}
	background := m.cfg.ActiveIndicator == config.ActiveBackground
	// The active and active-other-monitor rules differ only in the
	// state color.
	highlighted := func(cv config.ColorValue) {
		if background {
			out.bg = pick(cv)
			onAccent := m.resolve(mustToken(config.TokenFgOnAccent))
			out.label, out.icon, out.custom = onAccent, onAccent, onAccent
			return
		}
		c := pick(cv)
		out.underline = c
		out.label, out.icon, out.custom = c, c, c
	}
	switch {
	case active:
		highlighted(m.cfg.ActiveColor)
	case hasClass(model.classes, "active-other-monitor"):
		highlighted(m.otherMonitor)
	case hasClass(model.classes, "occupied"):
		c := pick(m.cfg.OccupiedColor)
		out.label, out.icon, out.custom = c, c, c
	case hasClass(model.classes, "empty"):
		// .workspace-icon is not in the empty rule; the custom icon and
		// the empty placeholder are.
		c := pick(m.cfg.EmptyColor)
		out.label, out.custom, out.emptyIcon = c, c, c
	}
	// &:hover:not(.active): the plain active color at 15%, never the
	// override.
	out.hoverBg = out.bg
	if !active {
		out.hoverBg = styling.ColorMix(m.resolve(m.cfg.ActiveColor), transparentColor, 15)
	}
	// .urgent:not(.urgent-application) outranks .special.
	switch {
	case hasClass(model.classes, "urgent") && !hasClass(model.classes, "urgent-application"):
		out.opacity = 0.5
	case hasClass(model.classes, "special"):
		out.opacity = 0.8
	}
	return out
}

// cwsButton is one `.workspace` button: a gelm Button with literal
// state backgrounds (BgExplicit: transparent at rest, the hover fill on
// hover), the content box centered in it, the active underline and
// inactive opacity painted around it, and the five input bindings
// routed from the hit leaf.
type cwsButton struct {
	*widget.Button
	m       *cwsView
	model   cwsButtonModel
	colors  cwsColors
	content *widget.Box
	minPx   int
}

func newCwsButton(m *cwsView, model cwsButtonModel) *cwsButton {
	b := &cwsButton{m: m, model: model}
	b.colors = m.cwsResolveColors(model)
	b.minPx = int(math.Round(cwsSpaceLgRem * styling.RemBase * m.scale()))
	b.content = b.buildContent()
	b.Button = widget.NewButton(b.content, 0, m.buttonRadius())
	b.BgExplicit = true
	b.Bg, b.BgHover, b.BgPressed = b.colors.bg, b.colors.hoverBg, b.colors.hoverBg
	b.OnClick = func() { m.dispatchClick(m.cfg.Click.LeftClick, model.ws) }
	b.AddClass(model.classes...)
	return b
}

// buildContent is the button view!: the identity row (label, mapped
// icon, divider) and the app-icon box, inside the workspace-content
// margins.
func (b *cwsButton) buildContent() *widget.Box {
	m, cfg := b.m, b.m.cfg
	vertical := m.vertical()
	axis := widget.Row
	if vertical {
		axis = widget.Column
	}
	content := widget.NewBox(axis, 0, 0)
	content.AddClass("workspace-content")

	labelPx := m.labelPx()
	if b.model.showLabel(cfg.DisplayMode) || b.model.showIcon(cfg.DisplayMode) || b.model.showDivider(cfg) {
		identity := widget.NewBox(widget.Row, 0, 0)
		if b.model.showLabel(cfg.DisplayMode) {
			label := widget.NewLabel(m.boldFace, labelPx, b.model.label, b.colors.label)
			label.AddClass("workspace-label")
			identity.Append(label, false)
		}
		if b.model.showIcon(cfg.DisplayMode) {
			icon := widget.NewThemeIcon(b.model.icon, m.iconPx())
			if b.model.flavor == cwsHyprland {
				icon.SetTint(b.colors.custom)
				icon.AddClass("workspace-custom-icon")
			} else {
				icon.SetTint(b.colors.icon)
				icon.AddClass("workspace-icon")
			}
			identity.Append(icon, false)
		}
		if b.model.showDivider(cfg) {
			divider := widget.NewLabel(m.boldFace, labelPx, cfg.Divider, b.colors.label)
			divider.AddClass("workspace-divider")
			identity.Append(divider, false)
		}
		content.Append(identity, false)
	}
	if cfg.AppIconsShow {
		icons := widget.NewBox(axis, m.iconGapPx(), 0)
		icons.AddClass("workspace-icons")
		if len(b.model.appIcons) == 0 {
			icon := widget.NewThemeIcon(cfg.AppIconsEmpty, m.iconPx())
			icon.SetTint(b.colors.emptyIcon)
			icon.AddClass("workspace-icon", "workspace-icon-empty")
			icons.Append(icon, false)
		}
		for _, app := range b.model.appIcons {
			icon := widget.NewThemeIcon(app.name, m.iconPx())
			icon.SetTint(b.colors.icon)
			icon.AddClass("workspace-icon")
			var w widget.Widget = icon
			if app.urgent {
				icon.AddClass("urgent")
				fader := widget.NewFader(icon)
				fader.SetOpacity(0.5)
				w = fader
			}
			icons.Append(w, false)
		}
		content.Append(icons, false)
	}
	pad := m.paddingPx()
	padded := widget.NewBox(widget.Row, 0, 0)
	padded.Append(content, true)
	if vertical {
		padded.SetPadding(render.Insets{Top: pad, Bottom: pad})
	} else {
		padded.SetPadding(render.Insets{Left: pad, Right: pad})
	}
	return padded
}

// Measure is the button grown to the --bar-space-lg minimum.
func (b *cwsButton) Measure(con widget.Constraints) widget.Size {
	sz := b.Button.Measure(con)
	return widget.Size{W: min(max(sz.W, b.minPx), con.Max.W), H: min(max(sz.H, b.minPx), con.Max.H)}
}

// Arrange centers the content in the button rect.
func (b *cwsButton) Arrange(r render.Rect) {
	b.Button.Arrange(r)
	sz := b.content.Measure(widget.Constraints{Max: widget.Size{W: r.W, H: r.H}})
	b.content.Arrange(render.Rect{
		X: r.X + (r.W-sz.W)/2,
		Y: r.Y + (r.H-sz.H)/2,
		W: sz.W,
		H: sz.H,
	})
}

func (b *cwsButton) ArrangeRoot(r render.Rect) { b.Arrange(r) }

func (b *cwsButton) Paint(cv *render.Canvas) {
	if b.colors.opacity < 1 {
		prev := cv.PushAlpha(b.colors.opacity)
		defer cv.PopAlpha(prev)
	}
	b.Button.Paint(cv)
	r := b.Bounds()
	if b.colors.underline != 0 {
		// linear-gradient(to top|right, color 2px, transparent 2px)
		if b.m.vertical() {
			cv.FillRect(render.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, b.colors.underline)
		} else {
			cv.FillRect(render.Rect{X: r.X, Y: r.Y + r.H - 2, W: r.W, H: 2}, b.colors.underline)
		}
	}
}

// HitTest keeps the workspace button the hit leaf, so the router's
// hover, click (OnClick: connect_clicked's LeftClick(id)), and
// pointer-button calls land here.
func (b *cwsButton) HitTest(p widget.Point) widget.Widget {
	if b.Button.HitTest(p) != nil {
		return b
	}
	return nil
}

// PointerButton is the middle/right GestureClick pair.
func (b *cwsButton) PointerButton(button uint32) {
	switch button {
	case widget.BTNMiddle:
		b.m.dispatchClick(b.m.cfg.Click.MiddleClick, b.model.ws)
	case widget.BTNRight:
		b.m.dispatchClick(b.m.cfg.Click.RightClick, b.model.ws)
	}
}

// ScrollInput is attach_scroll: discrete vertical steps, always
// consumed (Propagation::Stop).
func (b *cwsButton) ScrollInput(dy int) bool {
	switch {
	case dy > 0:
		b.m.dispatchScroll(b.m.cfg.Click.ScrollDown)
	case dy < 0:
		b.m.dispatchScroll(b.m.cfg.Click.ScrollUp)
	}
	return true
}
