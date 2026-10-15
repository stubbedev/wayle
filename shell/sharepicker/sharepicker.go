// Package sharepicker is the screencast source picker
// (crates/wayle-shell/src/shell/share_picker) the portal pops over
// com.wayle.SharePicker1: a layer surface with Windows, Outputs, and
// Region pages. A card or region confirms at once in single-select
// mode; in multi-select mode cards toggle into a pending set the Share
// button confirms. The reply is the XDPH selection suffix -
// `[r]/window:<id>`, `/screen:<name>`, `/region:<out>@x,y,w,h`, several
// payloads joined by `;` - or empty on cancel.
package sharepicker

import (
	"fmt"
	"image"
	"log"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/capture"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sharepreview"
	"github.com/stubbedev/wayle/shell/regionoverlay"
	"github.com/stubbedev/wayle/shell/reveal"
	"github.com/stubbedev/wayle/shell/widgets"
	"github.com/stubbedev/wayle/styling"
)

// escapeKeycode is the evdev code of Escape.
const escapeKeycode = 1

// labelPx and spinnerPx size the text and the loading spinner before
// the stylesheet's rules.
const (
	labelPx   = 13
	spinnerPx = 24
)

// The page names, which are also the notebook tab labels.
const (
	pageWindows = "Windows"
	pageOutputs = "Outputs"
	pageRegion  = "Region"
)

// Deps are the picker's collaborators.
type Deps struct {
	// Config is the live config: each request reads [share-picker],
	// the styling scale, and the animations afresh.
	Config func() *config.Config
	// Font shapes the labels; Sheet styles the surface.
	Font  render.Font
	Sheet *widget.Stylesheet
}

// Picker is the share picker component. Pick is its entry point; the
// rest runs on the gelm loop goroutine.
type Picker struct {
	app    *app.Application
	d      Deps
	region interface {
		Request(frames map[string]*image.RGBA) (regionoverlay.Selection, bool)
	}

	// cfg, anims, and scale are the request's config snapshot
	// (PickerConfig::from_config).
	cfg   config.SharePickerConfig
	anims config.AnimationsConfig
	scale float64

	reply      chan string
	allowToken bool
	multiple   bool
	pending    []string
	root       widget.Widget
	rev        *widget.Revealer
	layer      *app.LayerWindow
	confirm    *widget.Button
	confirmLbl *widget.Label

	// invoke marshals onto the loop (Application.Invoke).
	invoke func(func())
	// windows and outputs stand in for the compositor queries in
	// tests.
	windows func() []sharepreview.Toplevel
	outputs func() []outputInfo
}

// New builds the picker on the application; region is the live region
// overlay the Region page opens.
func New(a *app.Application, d Deps, region *regionoverlay.Overlay) *Picker {
	return &Picker{app: a, d: d, region: region, invoke: a.Invoke}
}

// Pick shows the picker for one portal request and blocks until the
// user selects or cancels. windowList is the raw
// XDPH_WINDOW_SHARING_LIST; allowToken seeds the restore-token box;
// multiple enables multi-select. A request arriving while one is open
// answers the older one with an empty (cancelled) reply. Call it from
// any goroutine but the loop's.
func (p *Picker) Pick(windowList string, allowToken, multiple bool) string {
	ch := make(chan string, 1)
	toplevels := sharepreview.ParseList(windowList)
	p.invoke(func() { p.show(ch, toplevels, allowToken, multiple) })
	return <-ch
}

// show replaces any open request, re-reads the config, and maps the
// surface with its enter transition.
func (p *Picker) show(ch chan string, toplevels []sharepreview.Toplevel, allowToken, multiple bool) {
	p.answer("")
	p.snapshot()
	p.reply, p.allowToken, p.multiple, p.pending = ch, allowToken, multiple, nil
	p.root = p.build(toplevels)
	if err := p.mapSurface(); err != nil {
		log.Printf("share picker: %v", err)
		p.answer("")
		return
	}
	reveal.Show(p.rev, p.anims, config.AnimSharePicker)
}

// snapshot reads the request's config.
func (p *Picker) snapshot() {
	cfg := p.d.Config()
	p.cfg, p.anims, p.scale = cfg.SharePicker, cfg.Animations, float64(cfg.Styling.Scale)
}

// px resolves a share-picker size against its rem base at the styling
// scale, truncated as the Rust `as i32`.
func (p *Picker) px(s config.Size, baseRem float64) int {
	return int(s.ResolvePx(baseRem*styling.RemBase, p.scale))
}

func (p *Picker) width() int  { return p.px(p.cfg.Width, config.SharePickerWidthBaseRem) }
func (p *Picker) height() int { return p.px(p.cfg.Height, config.SharePickerHeightBaseRem) }
func (p *Picker) widgetSize() int {
	return p.px(p.cfg.WidgetSize, config.SharePickerWidgetBaseRem)
}

// mapSurface maps the overlay: unanchored (centered by the compositor)
// and sized by the surface's size request.
func (p *Picker) mapSurface() error {
	layer, err := p.app.NewLayer(app.LayerConfig{
		Layer:         app.LayerOverlay,
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardOnDemand,
		Namespace:     "wayle-share-picker",
		Root:          p.root,
		OnKey: func(_ *widget.Router, keycode uint32, _ app.Mods) {
			if keycode == escapeKeycode {
				p.answer("")
			}
		},
	})
	if err != nil {
		return err
	}
	p.layer = layer
	return nil
}

// hide unmaps the surface at once (the Region page steps aside while
// the overlay is up).
func (p *Picker) hide() {
	if p.layer != nil {
		p.layer.Close()
		p.layer = nil
	}
}

// dismiss plays the exit transition, then unmaps (hide_animated).
func (p *Picker) dismiss() {
	layer := p.layer
	if layer == nil || p.rev == nil {
		p.hide()
		return
	}
	p.layer = nil
	reveal.Hide(p.rev, p.anims, config.AnimSharePicker, layer.Close)
}

// answer replies to the open request (once) and dismisses the
// surface.
func (p *Picker) answer(s string) {
	if p.reply != nil {
		p.reply <- s
		p.reply = nil
	}
	p.dismiss()
}

// prefix is the reply's flag segment: `r` asks the portal for a
// restore token.
func (p *Picker) prefix() string {
	if p.allowToken {
		return "r"
	}
	return ""
}

// selectPayload handles a card or region: in single-select mode it
// confirms at once; in multi-select mode it toggles the payload in the
// pending set.
func (p *Picker) selectPayload(payload string) {
	if !p.multiple {
		p.answer(p.prefix() + "/" + payload)
		return
	}
	if i := slices.Index(p.pending, payload); i >= 0 {
		p.pending = slices.Delete(p.pending, i, i+1)
	} else {
		p.pending = append(p.pending, payload)
	}
	if p.confirm != nil {
		p.confirm.SetEnabled(len(p.pending) > 0)
		p.confirmLbl.SetText(confirmLabel(len(p.pending)))
	}
}

// confirmPending answers the multi-select set, joined by the `;` the
// portal's multi-payload parser expects. An empty set does nothing.
func (p *Picker) confirmPending() {
	if len(p.pending) == 0 {
		return
	}
	p.answer(p.prefix() + "/" + strings.Join(p.pending, ";"))
}

// confirmLabel is the Share button's label for the pending count.
func confirmLabel(n int) string {
	if n <= 1 {
		return "Share"
	}
	return fmt.Sprintf("Share %d sources", n)
}

// windowPayload names a window card's selection: the stable ext
// identifier when the entry has one (a consumer owning its capture can
// re-resolve it), else the XDPH id only XDPH resolves.
func windowPayload(tl sharepreview.Toplevel) string {
	if tl.Identifier != "" {
		return "window:" + tl.Identifier
	}
	return "window:" + strconv.FormatUint(tl.ID, 10)
}

// regionPayload is a region selection's payload.
func regionPayload(sel regionoverlay.Selection) string {
	return fmt.Sprintf("region:%s@%d,%d,%d,%d", sel.Output, sel.X, sel.Y, sel.Width, sel.Height)
}

// build assembles the surface (the view!):
//
//	window.background.share-picker-window
//	└ revealer
//	  └ box.share-picker-surface (vertical, the configured size request)
//	    ├ notebook.share-picker-notebook (Windows, Outputs, Region)
//	    ├ the .share-picker-restore-button check and label
//	    └ button.share-picker-confirm-button (multi-select only)
func (p *Picker) build(toplevels []sharepreview.Toplevel) widget.Widget {
	nb := widget.NewNotebook(p.d.Font)
	nb.AddClass("share-picker-notebook")
	nb.SetVExpand(true)
	nb.AppendTab(pageWindows, p.windowsPage(toplevels))
	nb.AppendTab(pageOutputs, p.outputsPage())
	nb.AppendTab(pageRegion, p.regionPage())
	switch p.cfg.DefaultPage {
	case config.SharePickerOutputs:
		nb.SelectTab(pageOutputs)
	case config.SharePickerRegion:
		nb.SelectTab(pageRegion)
	default:
		nb.SelectTab(pageWindows)
	}
	surface := widget.NewBox(widget.Column, 0, 0)
	surface.AddClass("share-picker-surface")
	surface.SetSizeRequest(p.width(), p.height())
	surface.Append(nb, true)
	if !p.cfg.HideTokenRestore {
		check := widget.NewCheckButton(p.allowToken)
		check.SetCursorName("pointer")
		check.OnChanged = func(on bool) { p.allowToken = on }
		row := widget.NewBox(widget.Row, 0, 0)
		row.AddClass("share-picker-restore-button")
		row.Append(check, false)
		row.Append(p.label("Allow a restore token"), false)
		surface.Append(row, false)
	}
	p.confirm, p.confirmLbl = nil, nil
	if p.multiple {
		p.confirmLbl = p.label(confirmLabel(0))
		p.confirmLbl.SetAlignment(render.AlignCenter)
		p.confirm = widget.NewButton(p.confirmLbl, 0, 0)
		p.confirm.AddClass("share-picker-confirm-button")
		p.confirm.SetEnabled(false)
		p.confirm.OnClick = p.confirmPending
		surface.Append(p.confirm, false)
	}
	p.rev = widget.NewRevealer(surface)
	root := widget.NewBox(widget.Column, 0, 0)
	root.SetElement("window")
	root.AddClass("background", "share-picker-window")
	if p.d.Sheet != nil {
		root.AttachStylesheet(p.d.Sheet)
	}
	root.Append(p.rev, true)
	return root
}

func (p *Picker) label(text string) *widget.Label {
	return widget.NewLabel(p.d.Font, labelPx, text, 0)
}

// page is a notebook page: the content in a .share-picker-page scroll.
func page(content widget.Widget) *widget.Scroll {
	sc := widget.NewScroll(content)
	sc.AddClass("share-picker-page")
	sc.FillX, sc.FillY = true, true
	return sc
}

// placeholder is an empty page's centered, expanding message.
func (p *Picker) placeholder(text string) widget.Widget {
	l := p.label(text)
	l.AddClass("share-picker-placeholder")
	l.SetAlignment(render.AlignCenter)
	l.SetHExpand(true)
	l.SetVExpand(true)
	return page(widgets.Center(l))
}

// cardLabel is a card's one-line caption (max-width-chars 1: it takes
// the card's width and ellipsizes).
func (p *Picker) cardLabel(text, class string) *widget.Label {
	l := p.label(text)
	l.AddClass(class)
	l.SetEllipsize(widget.EllipsizeEnd)
	l.SetMaxWidthChars(1)
	return l
}

// windowsPage lays the window cards out in rows, as the homogeneous
// FlowBox does. The XDPH list feeds it; without one, the generic ext
// enumeration does. Cards run in reverse list order, as the Rust
// FlowBox inserted each at the front.
func (p *Picker) windowsPage(toplevels []sharepreview.Toplevel) widget.Widget {
	if len(toplevels) == 0 {
		enumerate := p.windows
		if enumerate == nil {
			enumerate = fallbackToplevels
		}
		toplevels = enumerate()
	}
	if len(toplevels) == 0 {
		return p.placeholder("No windows available")
	}
	spacing := p.px(p.cfg.WindowsSpacing, config.SharePickerWindowsSpacingBaseRem)
	cols := gridColumns(len(toplevels), int(p.cfg.WindowsMinPerRow), int(p.cfg.WindowsMaxPerRow))
	grid := widget.NewBox(widget.Column, spacing, 0)
	grid.SetElement("flowbox")
	var row *widget.Box
	for i := range toplevels {
		tl := toplevels[len(toplevels)-1-i]
		if i%cols == 0 {
			row = widget.NewBox(widget.Row, spacing, 0)
			row.SetHomogeneous(true)
			grid.Append(row, false)
		}
		row.Append(p.windowCard(tl), true)
	}
	return page(grid)
}

// windowCard is build_window_card: the preview (a spinner over it until
// the capture lands), the title (the class when the title is blank),
// and the class, the whole card clickable.
func (p *Picker) windowCard(tl sharepreview.Toplevel) widget.Widget {
	preview := widget.NewImage(nil)
	preview.AddClass("share-picker-image")
	preview.SetScale(widget.ImageFit)
	preview.SetSizeRequest(0, p.widgetSize())
	preview.SetVExpand(true)
	preview.SetVAlign(widget.AlignCenter)
	spinner := widget.NewSpinner(spinnerPx)
	spinner.SetSpinning(true)
	shot := widget.NewOverlay().Append(preview).Append(widgets.Center(spinner))
	card := widget.NewBox(widget.Column, 0, 0)
	card.AddClass("share-picker-card", "share-picker-card-loading")
	card.SetVAlign(widget.AlignStart)
	card.Append(shot, false)
	card.Append(p.cardLabel(cardTitle(tl), "share-picker-image-label"), false)
	card.Append(p.cardLabel(tl.Class, "share-picker-image-class-label"), false)
	child := widget.NewBox(widget.Column, 0, 0)
	child.SetElement("flowboxchild")
	child.Append(card, false)
	child.SetTooltip(tl.Title + "\n" + tl.Class)
	child.SetCursorName("pointer")
	payload := windowPayload(tl)
	child.SetOnClickWithin(func() { p.selectPayload(payload) })
	p.loadPreview(preview, card, spinner, func(size int) (*image.RGBA, error) { return windowThumb(tl, size) })
	return child
}

// cardTitle is a window card's title line: the title, or the class
// when the title is blank.
func cardTitle(tl sharepreview.Toplevel) string {
	if strings.TrimSpace(tl.Title) == "" {
		return tl.Class
	}
	return tl.Title
}

// loadPreview is update_frame_lazily: the capture runs off the loop and
// lands back on it; the spinner goes and the card drops its loading
// class either way. grab gets the request's resize size, read on the
// loop since the next request re-snapshots the config.
func (p *Picker) loadPreview(preview *widget.Image, card *widget.Box, spinner *widget.Spinner, grab func(size int) (*image.RGBA, error)) {
	size := int(p.cfg.ResizeSize)
	go func() {
		img, err := grab(size)
		p.invoke(func() {
			if spinner != nil {
				spinner.SetVisible(false)
			}
			card.RemoveClass("share-picker-card-loading")
			if err != nil {
				log.Printf("share picker: preview: %v", err)
				return
			}
			preview.SetImage(img)
		})
	}()
}

// outputsPage maps the outputs as the compositor lays them out.
func (p *Picker) outputsPage() widget.Widget {
	list := p.outputs
	if list == nil {
		list = func() []outputInfo {
			c, err := capture.Connect()
			if err != nil {
				log.Printf("share picker: outputs: %v", err)
				return nil
			}
			defer func() { _ = c.Close() }()
			return outputInfos(c.Outputs())
		}
	}
	infos := list()
	if len(infos) == 0 {
		return p.placeholder("No outputs available")
	}
	if p.cfg.OutputsRespectScaling {
		applyScaling(infos)
	}
	slots := make([]outputSlot, len(infos))
	for i, o := range infos {
		slots[i] = outputSlot{x: o.x, y: o.y, width: o.width, height: o.height}
	}
	m := &outputMap{
		area:    newMonitorArea(slots),
		slots:   slots,
		spacing: p.px(p.cfg.OutputsSpacing, config.SharePickerOutputsSpacingBaseRem),
	}
	for _, o := range infos {
		m.cards = append(m.cards, p.outputCard(o))
	}
	return page(m)
}

// outputCard is build_output_card: a button around the card with the
// output's frame filling it, and the connector name when configured.
func (p *Picker) outputCard(o outputInfo) widget.Widget {
	preview := widget.NewImage(nil)
	preview.AddClass("share-picker-image")
	preview.SetScale(widget.ImageStretch)
	preview.SetVExpand(true)
	card := widget.NewBox(widget.Column, 0, 0)
	card.AddClass("share-picker-card", "share-picker-card-loading")
	card.Append(preview, true)
	if p.cfg.OutputsShowLabel {
		card.Append(p.cardLabel(o.name, "share-picker-image-label"), false)
	}
	button := widget.NewButton(card, 0, 0)
	button.SetCursorName("pointer")
	payload := "screen:" + o.name
	button.OnClick = func() { p.selectPayload(payload) }
	p.loadPreview(preview, card, nil, func(size int) (*image.RGBA, error) { return outputThumb(o.name, size) })
	return button
}

// regionPage is build_region_page: the Select region button. The picker
// steps aside while the live overlay is up, then comes back to take the
// selection (or to stay open after a cancel).
func (p *Picker) regionPage() widget.Widget {
	lbl := p.label("Select region")
	lbl.SetAlignment(render.AlignCenter)
	btn := widget.NewButton(lbl, 0, 0)
	btn.AddClass("primary", "share-picker-region-button")
	btn.SetCursorName("pointer")
	btn.OnClick = func() {
		p.hide()
		go func() {
			sel, ok := p.region.Request(nil)
			p.invoke(func() {
				if p.reply == nil {
					return // the request was answered meanwhile
				}
				if err := p.mapSurface(); err != nil {
					log.Printf("share picker: %v", err)
				}
				if ok {
					p.selectPayload(regionPayload(sel))
				}
			})
		}()
	}
	box := widget.NewBox(widget.Column, 0, 0)
	box.AddClass("share-picker-page")
	box.Append(btn, false)
	return page(widgets.Center(box))
}
