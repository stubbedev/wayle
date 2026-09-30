// Package popups renders the notification popup window: a stack of
// cards on one output, following the notification service's feed,
// auto-dismissing after the configured duration, hidden while DND.
package popups

import (
	"sync"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/styling"
)

// Popups owns the popup surface for one output.
type Popups struct {
	app   *app.Application
	svc   *notifications.Service
	cfg   config.NotificationConfig
	font  render.Font
	pal   *styling.Palette
	out   *app.Output
	mu    sync.Mutex
	win   *app.LayerWindow
	root  *widget.Box
	cards map[uint32]*card
	order []uint32
}

// card is one popup's widgets; root is what the stack holds (the box,
// or its hover wrapper under popup-hover-pause).
type card struct {
	id    uint32
	icon  *widget.Icon
	title *widget.Label
	body  *widget.Label
	box   *widget.Box
	root  widget.Widget
}

// New builds the popup host over one output. The service's popup
// countdowns take the configured popup-duration.
func New(application *app.Application, svc *notifications.Service, cfg config.NotificationConfig, font render.Font, pal *styling.Palette, out *app.Output) *Popups {
	svc.SetPopupDuration(time.Duration(cfg.PopupDurationMS) * time.Millisecond)
	return &Popups{
		app:   application,
		svc:   svc,
		cfg:   cfg,
		font:  font,
		pal:   pal,
		out:   out,
		cards: make(map[uint32]*card),
	}
}

// Run follows the service's feed until the process exits; each change
// reconciles on the loop goroutine, which owns the widget tree.
func (p *Popups) Run() {
	for range p.svc.Subscribe() {
		if p.app == nil {
			p.sync()
			continue
		}
		p.app.Invoke(p.sync)
	}
}

// sync reconciles the visible card stack with the service's popup set.
// Headless construction tracks the card set; only the window mapping
// needs the application.
func (p *Popups) sync() {
	visible := p.svc.Popups()
	if len(visible) > p.cfg.PopupMaxVisible {
		visible = visible[:p.cfg.PopupMaxVisible]
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	order := make([]uint32, 0, len(visible))
	live := make(map[uint32]bool, len(visible))
	for _, n := range visible {
		live[n.ID] = true
		order = append(order, n.ID)
		if _, ok := p.cards[n.ID]; !ok {
			p.addLocked(n)
		}
	}
	for id := range p.cards {
		if !live[id] {
			delete(p.cards, id)
		}
	}
	// The service lists popups newest first.
	p.order = order
	p.layoutLocked()
}

// addLocked builds a card for one notification. The caller holds mu.
func (p *Popups) addLocked(n *notifications.Notification) {
	fg, _ := p.pal.Token(config.TokenFgDefault)
	c := &card{
		id:    n.ID,
		icon:  widget.NewThemeIcon(notifPopupIcon(n), 20),
		title: widget.NewLabel(p.font, 14, n.Summary, fg),
		body:  widget.NewLabel(p.font, 12, n.Body, fg),
	}
	col := widget.NewBox(widget.Column, 4, 0)
	col.Append(c.title, false)
	col.Append(c.body, false)
	row := widget.NewBox(widget.Row, 10, 0)
	row.Append(c.icon, false)
	row.Append(col, true)
	c.box = widget.NewBox(widget.Column, 0, 10)
	c.box.Append(row, false)
	c.root = c.box
	if p.cfg.PopupHoverPause {
		// The countdown lives in the service; hovering pauses it and
		// leaving resumes it with the time that was left.
		id := n.ID
		c.root = newHoverCard(c.box, func(on bool) {
			if on {
				p.svc.InhibitPopup(id)
				return
			}
			p.svc.ReleasePopup(id)
		})
	}
	p.cards[n.ID] = c
}

// layoutLocked rebuilds the root stack. The caller holds mu.
func (p *Popups) layoutLocked() {
	if len(p.cards) == 0 {
		if p.win != nil {
			win := p.win
			p.win = nil
			p.root = nil
			win.Close()
		}
		return
	}
	if p.win == nil && p.app != nil {
		win, root, err := p.ensureWindow()
		if err != nil {
			return
		}
		p.win = win
		p.root = root
	}
	if p.root == nil {
		// Headless (or window creation failed): the card set tracks the
		// popup state without a surface.
		return
	}
	p.root.Clear()
	for _, id := range p.stackOrder() {
		if c, ok := p.cards[id]; ok {
			p.root.Append(c.root, false)
		}
	}
}

// stackOrder is the top-to-bottom card order: newest-first prepends
// each new card (the service's newest-first list as is), oldest-first
// appends it (the list reversed) — insert_new_cards's use_prepend.
func (p *Popups) stackOrder() []uint32 {
	if p.cfg.PopupStacking != "oldest-first" {
		return p.order
	}
	ids := make([]uint32, len(p.order))
	for i, id := range p.order {
		ids[len(p.order)-1-i] = id
	}
	return ids
}

// ensureWindow maps the popup surface at the configured position.
func (p *Popups) ensureWindow() (*app.LayerWindow, *widget.Box, error) {
	bg, _ := p.pal.Token(config.TokenBgSurface)
	root := widget.NewBox(widget.Column, int(p.cfg.PopupGap*16), 8)
	win, err := p.app.NewLayer(app.LayerConfig{
		Output:        p.out,
		Layer:         app.LayerOverlay,
		Anchor:        popupsAnchors(p.cfg.PopupPosition),
		Width:         380,
		ExclusiveZone: -1,
		Namespace:     "wayle-popups",
		Root:          root,
		Background:    bg,
	})
	return win, root, err
}

// Output picks the popup monitor: "primary" is the first output, a
// connector name its output, falling back to the first when the
// connector is not connected (layer_shell.rs apply_monitor_by_connector).
// Nil only when there are no outputs.
func Output(outputs []*app.Output, monitor string) *app.Output {
	if len(outputs) == 0 {
		return nil
	}
	if monitor != "primary" {
		for _, out := range outputs {
			if out.Name == monitor {
				return out
			}
		}
	}
	return outputs[0]
}

// popupsAnchors maps the position onto anchors; both edges of the
// cross axis so the window hugs the corner.
func popupsAnchors(position string) app.Anchor {
	anchor, _ := popupsAnchorOK(position)
	return anchor
}

func popupsAnchorOK(position string) (app.Anchor, bool) {
	// Reuse the OSD's corner/edge table; popups accept the corners and
	// the left/right sides (the schema's popup-position set).
	switch position {
	case config.OsdTopLeft, config.OsdTopRight, config.OsdBottomLeft, config.OsdBottomRight, config.OsdLeft, config.OsdRight:
		anchor, ok := popupCornerAnchors[position]
		return anchor, ok
	}
	return 0, false
}

var popupCornerAnchors = map[string]app.Anchor{
	config.OsdTopLeft:     app.AnchorTop | app.AnchorLeft,
	config.OsdTopRight:    app.AnchorTop | app.AnchorRight,
	config.OsdBottomLeft:  app.AnchorBottom | app.AnchorLeft,
	config.OsdBottomRight: app.AnchorBottom | app.AnchorRight,
	config.OsdLeft:        app.AnchorLeft | app.AnchorTop | app.AnchorBottom,
	config.OsdRight:       app.AnchorRight | app.AnchorTop | app.AnchorBottom,
}

// notifPopupIcon picks the card's glyph: the sender's app icon when
// named, the bell otherwise.
func notifPopupIcon(n *notifications.Notification) string {
	if n.AppIcon != "" {
		return n.AppIcon
	}
	return "ld-bell-symbolic"
}

// Visible reports the card count (tests).
func (p *Popups) Visible() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.cards)
}
