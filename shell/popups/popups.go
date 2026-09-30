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

// card is one popup's widgets.
type card struct {
	id    uint32
	icon  *widget.Icon
	title *widget.Label
	body  *widget.Label
	box   *widget.Box
}

// New builds the popup host over one output.
func New(application *app.Application, svc *notifications.Service, cfg config.NotificationConfig, font render.Font, pal *styling.Palette, out *app.Output) *Popups {
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

// Run follows the service's feed until the process exits.
func (p *Popups) Run() {
	for range p.svc.Events() {
		p.sync()
	}
}

// sync reconciles the visible card stack with the service's popup set.
// Headless construction tracks the card set; only the window mapping
// needs the application.
func (p *Popups) sync() {
	visible := p.svc.Popups()
	p.mu.Lock()
	defer p.mu.Unlock()
	live := make(map[uint32]bool, len(visible))
	shown := 0
	for _, n := range visible {
		if shown >= p.cfg.PopupMaxVisible {
			break
		}
		shown++
		live[n.ID] = true
		if _, ok := p.cards[n.ID]; !ok {
			p.addLocked(n)
		}
	}
	for _, id := range p.order {
		if !live[id] {
			p.removeLocked(id)
		}
	}
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
	p.cards[n.ID] = c
	p.order = append(p.order, n.ID)
	// The popup's own deadline: the notification's expiry when it has
	// one, otherwise the configured popup duration. A zero timeout
	// sticks until dismissed.
	delay := time.Duration(p.cfg.PopupDurationMS) * time.Millisecond
	if n.ExpireMS > 0 {
		delay = time.Duration(n.ExpireMS) * time.Millisecond
	}
	if delay > 0 {
		go func(id uint32, d time.Duration) {
			time.Sleep(d)
			p.timedOut(id)
		}(n.ID, delay)
	}
}

// timedOut handles a card's deadline: the close behavior decides
// whether it leaves the history or just the screen.
func (p *Popups) timedOut(id uint32) {
	if p.cfg.PopupCloseAction == "close" {
		p.svc.Close(id, notifications.ClosedCall)
	}
	p.sync()
}

// removeLocked drops a card. The caller holds mu.
func (p *Popups) removeLocked(id uint32) {
	delete(p.cards, id)
	for i, v := range p.order {
		if v == id {
			p.order = append(p.order[:i], p.order[i+1:]...)
			break
		}
	}
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
	// The stacking order decides which end is newest; newest-first puts
	// new cards at the top.
	ids := p.order
	if p.cfg.PopupStacking == "oldest-first" {
		ids = make([]uint32, len(p.order))
		for i, id := range p.order {
			ids[len(p.order)-1-i] = id
		}
	}
	p.root.Clear()
	for _, id := range ids {
		if c, ok := p.cards[id]; ok {
			p.root.Append(c.box, false)
		}
	}
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
