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
	"github.com/stubbedev/wayle/styling"
)

// escapeKeycode is the evdev code of Escape.
const escapeKeycode = 1

// The page names, which are also the notebook tab labels.
const (
	pageWindows = "Windows"
	pageOutputs = "Outputs"
	pageRegion  = "Region"
)

// Style carries the themed pieces the picker paints with.
type Style struct {
	Font    render.Font
	LabelPx float64
	Palette *styling.Palette
}

// Picker is the share picker component. Pick is its entry point; the
// rest runs on the gelm loop goroutine.
type Picker struct {
	app    *app.Application
	cfg    config.SharePickerConfig
	style  Style
	region interface {
		Request(frames map[string]*image.RGBA) (regionoverlay.Selection, bool)
	}

	reply      chan string
	allowToken bool
	multiple   bool
	pending    []string
	root       widget.Widget
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
func New(a *app.Application, cfg config.SharePickerConfig, style Style, region *regionoverlay.Overlay) *Picker {
	return &Picker{app: a, cfg: cfg, style: style, region: region, invoke: a.Invoke}
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

// show replaces any open request and maps the surface.
func (p *Picker) show(ch chan string, toplevels []sharepreview.Toplevel, allowToken, multiple bool) {
	p.answer("")
	p.reply, p.allowToken, p.multiple, p.pending = ch, allowToken, multiple, nil
	p.root = p.build(toplevels)
	if err := p.mapSurface(); err != nil {
		log.Printf("share picker: %v", err)
		p.answer("")
	}
}

// width and height resolve the configured surface size (the Rust
// PickerConfig against styling scale 1: the Go shell does not parse
// [styling] yet).
func (p *Picker) width() int {
	return int(p.cfg.Width.ResolvePx(config.SharePickerWidthBaseRem*styling.RemBase, 1))
}

func (p *Picker) height() int {
	return int(p.cfg.Height.ResolvePx(config.SharePickerHeightBaseRem*styling.RemBase, 1))
}

func (p *Picker) mapSurface() error {
	layer, err := p.app.NewLayer(app.LayerConfig{
		Layer:         app.LayerOverlay,
		Width:         uint32(p.width()),
		Height:        uint32(p.height()),
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardOnDemand,
		Namespace:     "wayle-share-picker",
		Root:          p.root,
		Background:    p.style.Palette.Surface,
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

// hide unmaps the surface, keeping the tree for a later remap.
func (p *Picker) hide() {
	if p.layer != nil {
		p.layer.Close()
		p.layer = nil
	}
}

// answer replies to the open request (once) and hides the surface.
func (p *Picker) answer(s string) {
	if p.reply != nil {
		p.reply <- s
		p.reply = nil
	}
	p.hide()
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

// build assembles the surface: the notebook, the restore-token box,
// and (multi-select only) the Share button.
func (p *Picker) build(toplevels []sharepreview.Toplevel) widget.Widget {
	nb := widget.NewNotebook(p.style.Font)
	nb.AddClass("share-picker-notebook")
	nb.AppendTab(pageWindows, widget.NewScroll(p.windowsPage(toplevels)))
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
	surface := widget.NewBox(widget.Column, 8, 12)
	surface.AddClass("share-picker-surface")
	surface.Append(nb, true)
	if !p.cfg.HideTokenRestore {
		check := widget.NewCheckButton(p.allowToken)
		check.AddClass("share-picker-restore-button")
		check.OnChanged = func(on bool) { p.allowToken = on }
		row := widget.NewBox(widget.Row, 8, 0)
		row.Append(check, false)
		row.Append(p.label("Allow a restore token"), false)
		surface.Append(row, false)
	}
	p.confirm, p.confirmLbl = nil, nil
	if p.multiple {
		p.confirmLbl = p.label(confirmLabel(0))
		p.confirm = widget.NewButton(p.confirmLbl, 8, 6)
		p.confirm.AddClass("share-picker-confirm-button")
		p.confirm.SetEnabled(false)
		p.confirm.OnClick = p.confirmPending
		surface.Append(p.confirm, false)
	}
	return &sized{child: surface, w: p.width(), h: p.height()}
}

func (p *Picker) label(text string) *widget.Label {
	return widget.NewLabel(p.style.Font, p.style.LabelPx, text, p.style.Palette.Fg)
}

// placeholder is an empty page's centered message.
func (p *Picker) placeholder(text string) widget.Widget {
	l := p.label(text)
	l.AddClass("share-picker-placeholder")
	l.SetAlignment(render.AlignCenter)
	return l
}

// windowsPage lays the window cards out in rows. The XDPH list feeds
// it; without one, the generic ext enumeration does. Cards run in
// reverse list order, as the Rust FlowBox inserted each at the front.
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
	spacing := int(p.cfg.WindowsSpacing.ResolvePx(config.SharePickerWindowsSpacingBaseRem*styling.RemBase, 1))
	cols := gridColumns(len(toplevels), p.cfg.WindowsMinPerRow, p.cfg.WindowsMaxPerRow)
	page := widget.NewBox(widget.Column, spacing, 0)
	page.AddClass("share-picker-page")
	var row *widget.Box
	for i := range toplevels {
		tl := toplevels[len(toplevels)-1-i]
		if i%cols == 0 {
			row = widget.NewBox(widget.Row, spacing, 0)
			page.Append(row, false)
		}
		row.Append(p.windowCard(tl), true)
	}
	return page
}

// windowCard is one window: its preview (captured in the background),
// title (the class when the title is blank), and class.
func (p *Picker) windowCard(tl sharepreview.Toplevel) widget.Widget {
	title := cardTitle(tl)
	preview := widget.NewImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	preview.AddClass("share-picker-image")
	body := widget.NewBox(widget.Column, 4, 6)
	body.AddClass("share-picker-card", "share-picker-card-loading")
	body.Append(&sized{child: preview, h: p.widgetSize()}, false)
	titleLbl := p.label(title)
	titleLbl.AddClass("share-picker-image-label")
	titleLbl.SetEllipsize(widget.EllipsizeEnd)
	classLbl := p.label(tl.Class)
	classLbl.AddClass("share-picker-image-class-label")
	classLbl.SetEllipsize(widget.EllipsizeEnd)
	body.Append(titleLbl, false)
	body.Append(classLbl, false)
	card := widget.NewButton(body, 0, 8)
	card.AddClass("share-picker-card-button")
	card.SetTooltip(tl.Title + "\n" + tl.Class)
	payload := windowPayload(tl)
	card.OnClick = func() { p.selectPayload(payload) }
	p.loadPreview(preview, body, func() (*image.RGBA, error) { return windowThumb(tl, p.cfg.ResizeSize) })
	return card
}

// cardTitle is a window card's title line: the title, or the class
// when the title is blank.
func cardTitle(tl sharepreview.Toplevel) string {
	if strings.TrimSpace(tl.Title) == "" {
		return tl.Class
	}
	return tl.Title
}

func (p *Picker) widgetSize() int {
	return int(p.cfg.WidgetSize.ResolvePx(config.SharePickerWidgetBaseRem*styling.RemBase, 1))
}

// loadPreview captures off the loop and lands the image back on it;
// the card drops its loading class either way.
func (p *Picker) loadPreview(preview *widget.Image, body *widget.Box, grab func() (*image.RGBA, error)) {
	go func() {
		img, err := grab()
		p.invoke(func() {
			body.RemoveClass("share-picker-card-loading")
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
		spacing: int(p.cfg.OutputsSpacing.ResolvePx(config.SharePickerOutputsSpacingBaseRem*styling.RemBase, 1)),
	}
	m.AddClass("share-picker-page")
	for _, o := range infos {
		m.cards = append(m.cards, p.outputCard(o))
	}
	return m
}

// outputCard is one output: its preview filling the card, and the
// connector name when configured.
func (p *Picker) outputCard(o outputInfo) widget.Widget {
	preview := widget.NewImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	preview.AddClass("share-picker-image")
	preview.SetScale(widget.ImageStretch)
	body := widget.NewBox(widget.Column, 4, 0)
	body.AddClass("share-picker-card", "share-picker-card-loading")
	body.Append(preview, true)
	if p.cfg.OutputsShowLabel {
		l := p.label(o.name)
		l.AddClass("share-picker-image-label")
		l.SetEllipsize(widget.EllipsizeEnd)
		body.Append(l, false)
	}
	card := widget.NewButton(body, 0, 8)
	card.AddClass("share-picker-card-button")
	payload := "screen:" + o.name
	card.OnClick = func() { p.selectPayload(payload) }
	p.loadPreview(preview, body, func() (*image.RGBA, error) { return outputThumb(o.name, p.cfg.ResizeSize) })
	return card
}

// regionPage is the Select region button: the picker steps aside while
// the live overlay is up, then comes back to take the selection (or to
// stay open after a cancel).
func (p *Picker) regionPage() widget.Widget {
	btn := widget.NewButton(p.label("Select region"), 12, 8)
	btn.AddClass("primary", "share-picker-region-button")
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
	page := widget.NewBox(widget.Column, 0, 0)
	page.AddClass("share-picker-page")
	page.Append(btn, false)
	return page
}
