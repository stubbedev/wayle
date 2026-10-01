// Package popups renders the notification popup window: a stack of
// cards on one output, following the notification service's feed,
// auto-dismissing after the configured duration, hidden while DND.
package popups

import (
	"math"
	"sync"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/shell/layering"
	"github.com/stubbedev/wayle/shell/notifyui"
	"github.com/stubbedev/wayle/styling"
)

// Card geometry (templates.rs, card/methods.rs).
const (
	cardMaxWidthChars = 40
	cardBodyLines     = 3
	cardIconPx        = 32
	cardWidthPx       = 380
	// popupGapBasePx and popupMarginBasePx are the bases the popup-gap
	// and popup-margin-x/y multipliers scale (POPUP_GAP_BASE_REM 0.5,
	// POPUP_MARGIN_BASE_REM 0.75, at 16px a rem).
	popupGapBasePx    = 8
	popupMarginBasePx = 12
)

// Popups owns the popup surface for one output.
type Popups struct {
	app   *app.Application
	svc   *notifications.Service
	cfg   *config.Config
	font  render.Font
	pal   *styling.Palette
	sheet *widget.Stylesheet
	out   *app.Output
	now   func() time.Time
	mu    sync.Mutex
	win   *app.LayerWindow
	root  *widget.Box
	cards map[uint32]*card
	order []uint32
}

// card is one popup (NotificationPopupCard): the icon, the app and
// age over the summary and the body, the close button, and the action
// rows.
type card struct {
	id      uint32
	root    *widget.Box
	iconBox *widget.Box
	app     *widget.Label
	time    *widget.Label
	title   *widget.Label
	body    *widget.Label
	close   *widget.Button
	actions *widget.Box
}

// New builds the popup host over one output. The service's popup
// countdowns take the configured popup-duration; sheet is the shell's
// stylesheet, which styles the cards (nil leaves them unstyled).
func New(application *app.Application, svc *notifications.Service, cfg *config.Config, font render.Font, pal *styling.Palette, sheet *widget.Stylesheet, out *app.Output) *Popups {
	svc.SetPopupDuration(time.Duration(cfg.Notification.PopupDuration) * time.Millisecond)
	return &Popups{
		app:   application,
		svc:   svc,
		cfg:   cfg,
		font:  font,
		pal:   pal,
		sheet: sheet,
		out:   out,
		now:   time.Now,
		cards: make(map[uint32]*card),
	}
}

// SetConfig applies a reloaded config on the loop goroutine: the
// surface closes and the stack rebuilds at the new position, layer,
// gap, limits, and card behavior.
func (p *Popups) SetConfig(cfg *config.Config) {
	p.svc.SetPopupDuration(time.Duration(cfg.Notification.PopupDuration) * time.Millisecond)
	p.mu.Lock()
	p.cfg = cfg
	win := p.win
	p.win, p.root = nil, nil
	p.cards = make(map[uint32]*card)
	p.mu.Unlock()
	if win != nil {
		win.Close()
	}
	p.sync()
}

// Run follows the service's feed until the process exits; each change
// reconciles on the loop goroutine, which owns the widget tree.
func (p *Popups) Run() {
	events, _ := p.svc.Subscribe()
	for range events {
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
	p.mu.Lock()
	defer p.mu.Unlock()
	if limit := int(p.cfg.Notification.PopupMaxVisible); len(visible) > limit {
		visible = visible[:limit]
	}
	order := make([]uint32, 0, len(visible))
	live := make(map[uint32]bool, len(visible))
	for _, n := range visible {
		live[n.ID] = true
		order = append(order, n.ID)
		if _, ok := p.cards[n.ID]; !ok {
			p.cards[n.ID] = p.newCard(n)
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

// newCard builds one card (the view! of card/mod.rs and its init).
func (p *Popups) newCard(n *notifications.Notification) *card {
	cfg := p.cfg.Notification
	fg, _ := p.pal.Token(config.TokenFgDefault)
	muted, _ := p.pal.Token(config.TokenFgMuted)
	c := &card{id: n.ID}
	c.root = widget.NewBox(widget.Column, 8, 12)
	c.root.AddClass("notification-popup-card", notifyui.UrgencyClass(n.Urgency))
	if cfg.PopupShadow {
		c.root.AddClass("shadow")
	}
	if notifyui.UrgencyBarVisible(n.Urgency, cfg.PopupUrgencyBar) {
		c.root.AddClass("urgency-bar")
	}

	row := widget.NewBox(widget.Row, 10, 0)
	row.AddClass("notification-popup-content")
	icon := notifyui.ResolveIcon(cfg.IconSource, n)
	c.iconBox = widget.NewBox(widget.Row, 0, 0)
	c.iconBox.AddClass("notification-popup-icon")
	if notifyui.IsFileIcon(icon) {
		c.iconBox.AddClass("file-icon")
	}
	glyph := notifyui.NewIcon(icon, cardIconPx, fg)
	glyph.AddClass("notification-popup-icon-img")
	c.iconBox.Append(glyph, false)
	row.Append(c.iconBox, false)

	text := widget.NewBox(widget.Column, 2, 0)
	text.AddClass("notification-popup-text")
	header := widget.NewBox(widget.Row, 6, 0)
	header.AddClass("notification-popup-header")
	appName := n.AppName
	if appName == "" {
		appName = i18n.T("notification-popup-unknown-app")
	}
	c.app = widget.NewLabel(p.font, 11, appName, muted)
	c.app.AddClass("notification-popup-app")
	c.app.SetEllipsize(widget.EllipsizeEnd)
	header.Append(c.app, true)
	c.time = widget.NewLabel(p.font, 11, notifyui.TimeLabel(notifyui.RelativeTime(p.now(), n.Added), "notification-popup"), muted)
	c.time.AddClass("notification-popup-time")
	header.Append(c.time, false)
	text.Append(header, false)
	c.title = widget.NewLabel(p.font, 14, n.Summary, fg)
	c.title.AddClass("notification-popup-title")
	c.title.SetEllipsize(widget.EllipsizeEnd)
	c.title.SetMaxWidthChars(cardMaxWidthChars)
	text.Append(c.title, false)
	c.body = widget.NewLabel(p.font, 12, notifyui.BodyText(n.Body), muted)
	c.body.AddClass("notification-popup-body")
	c.body.SetWrap(true)
	c.body.SetMaxWidthChars(cardMaxWidthChars)
	c.body.SetMaxLines(cardBodyLines)
	c.body.SetEllipsize(widget.EllipsizeEnd)
	c.body.SetVisible(n.Body != "")
	text.Append(c.body, false)
	row.Append(text, true)

	x := widget.NewThemeIcon("window-close-symbolic", 14)
	x.SetTint(muted)
	c.close = widget.NewButton(x, 4, 6)
	c.close.AddClass("notification-popup-close")
	id := n.ID
	c.close.OnClick = func() { p.closeCard(id) }
	row.Append(c.close, false)
	c.root.Append(row, false)

	c.actions = p.actionRows(n)
	c.root.Append(c.actions, false)

	// A default action opens the card on a click anywhere its buttons
	// do not take (setup_default_action's gesture on the root).
	if _, ok := n.DefaultAction(); ok {
		c.root.SetOnClickWithin(func() { p.invokeDefault(id) })
	}
	if cfg.PopupHoverPause {
		// The countdown lives in the service; hovering pauses it and
		// leaving resumes it with the time that was left.
		c.root.SetOnHoverWithin(func(on bool) {
			if on {
				p.svc.InhibitPopup(id)
				return
			}
			p.svc.ReleasePopup(id)
		})
	}
	return c
}

// actionRows is setup_action_buttons: the non-default actions, three
// to a row, each row's buttons sharing its width; hidden without any.
func (p *Popups) actionRows(n *notifications.Notification) *widget.Box {
	fg, _ := p.pal.Token(config.TokenFgDefault)
	box := widget.NewBox(widget.Column, 4, 0)
	box.AddClass("notification-popup-actions")
	actions := notifyui.VisibleActions(n)
	box.SetVisible(len(actions) > 0)
	for start := 0; start < len(actions); start += notifyui.ActionsPerRow {
		row := widget.NewBox(widget.Row, 4, 0)
		row.AddClass("notification-popup-action-row")
		for _, a := range actions[start:min(start+notifyui.ActionsPerRow, len(actions))] {
			label := widget.NewLabel(p.font, 12, a.Label, fg)
			label.SetAlignment(render.AlignCenter)
			b := widget.NewButton(label, 6, 6)
			b.AddClass("notification-popup-action-btn")
			id, key := n.ID, a.ID
			b.OnClick = func() { p.invokeAction(id, key) }
			row.Append(b, true)
		}
		box.Append(row, false)
	}
	return box
}

// closeCard is the close button under popup-close-behavior: dismiss
// hides the popup and keeps the notification in the history, remove
// deletes it.
func (p *Popups) closeCard(id uint32) {
	if p.cfg.Notification.PopupCloseBehavior == config.PopupCloseBehaviorRemove {
		p.svc.Close(id, notifications.Dismissed)
		return
	}
	p.svc.DismissPopup(id)
}

// invokeAction is an action button: the popup goes, the action is
// invoked, and the notification is dismissed (build_action_button).
func (p *Popups) invokeAction(id uint32, key string) {
	p.svc.DismissPopup(id)
	p.svc.InvokeAction(id, key)
	p.svc.Close(id, notifications.Dismissed)
}

// invokeDefault is the card's click: the default action, then the
// close behavior. Invoked first, while the notification still exists
// for the service to emit ActionInvoked on.
func (p *Popups) invokeDefault(id uint32) {
	p.svc.InvokeAction(id, notifications.DefaultActionID)
	p.closeCard(id)
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
	if p.cfg.Notification.PopupStackingOrder != config.StackingOrderOldestFirst {
		return p.order
	}
	ids := make([]uint32, len(p.order))
	for i, id := range p.order {
		ids[len(p.order)-1-i] = id
	}
	return ids
}

// ensureWindow maps the popup surface at the configured position,
// margins, and layer. The host is transparent: each card draws its own
// plate from the stylesheet.
func (p *Popups) ensureWindow() (*app.LayerWindow, *widget.Box, error) {
	cfg := p.cfg.Notification
	scale := float64(p.cfg.Styling.Scale)
	root := widget.NewBox(widget.Column, int(math.Round(cfg.PopupGap.ResolvePx(popupGapBasePx, scale))), 8)
	root.AddClass("notification-popup-list")
	if p.sheet != nil {
		root.AttachStylesheet(p.sheet)
	}
	win, err := p.app.NewLayer(app.LayerConfig{
		Output:        p.out,
		Layer:         layering.For(p.cfg.General, cfg.PopupLayer),
		Anchor:        popupAnchors(cfg.PopupPosition),
		Margin:        popupMargins(cfg, scale),
		Width:         cardWidthPx,
		ExclusiveZone: -1,
		Namespace:     "wayle-notification-popup",
		Root:          root,
	})
	return win, root, err
}

// popupMargins is apply_position's margins: popup-margin-y on the
// anchored top or bottom edge, popup-margin-x on the anchored side.
func popupMargins(cfg config.NotificationConfig, scale float64) app.Margins {
	mx := int32(math.Round(cfg.PopupMarginX.ResolvePx(popupMarginBasePx, scale)))
	my := int32(math.Round(cfg.PopupMarginY.ResolvePx(popupMarginBasePx, scale)))
	var m app.Margins
	anchors := popupAnchors(cfg.PopupPosition)
	if anchors&app.AnchorTop != 0 {
		m.Top = my
	}
	if anchors&app.AnchorBottom != 0 {
		m.Bottom = my
	}
	if anchors&app.AnchorLeft != 0 {
		m.Left = mx
	}
	if anchors&app.AnchorRight != 0 {
		m.Right = mx
	}
	return m
}

// Output picks the popup monitor: "primary" is the first output, a
// connector name its output, falling back to the first when the
// connector is not connected (layer_shell.rs apply_monitor_by_connector).
// Nil only when there are no outputs.
func Output(outputs []*app.Output, monitor config.PopupMonitor) *app.Output {
	if len(outputs) == 0 {
		return nil
	}
	if !monitor.IsPrimary() {
		for _, out := range outputs {
			if out.Name == monitor.Connector() {
				return out
			}
		}
	}
	return outputs[0]
}

// popupAnchors maps the position onto layer-shell anchors
// (notification_popup/methods.rs apply_position): corners take both
// edges, centered positions only the one edge.
func popupAnchors(position config.PopupPosition) app.Anchor {
	switch position {
	case config.PopupPositionTopLeft:
		return app.AnchorTop | app.AnchorLeft
	case config.PopupPositionTopCenter:
		return app.AnchorTop
	case config.PopupPositionTopRight:
		return app.AnchorTop | app.AnchorRight
	case config.PopupPositionBottomLeft:
		return app.AnchorBottom | app.AnchorLeft
	case config.PopupPositionBottomCenter:
		return app.AnchorBottom
	case config.PopupPositionBottomRight:
		return app.AnchorBottom | app.AnchorRight
	case config.PopupPositionCenterLeft:
		return app.AnchorLeft
	case config.PopupPositionCenterRight:
		return app.AnchorRight
	}
	return app.AnchorTop | app.AnchorRight
}

// Visible reports the card count (tests).
func (p *Popups) Visible() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.cards)
}
