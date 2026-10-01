package bar

import (
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/shell/notifyui"
)

// Notification dropdown constants (notification_group/methods.rs,
// notification_item/methods.rs, watchers.rs).
const (
	notifMaxVisibleItems = 3
	notifIconPx          = 32
	notifTimeTick        = 30 * time.Second
)

// notifGroupData is one app's notifications, newest first.
type notifGroupData struct {
	app    string
	notifs []*notifications.Notification
}

// groupByApp is group_by_app: one group per app name, each newest
// first, the groups ordered by their newest notification.
func groupByApp(notifs []*notifications.Notification) []notifGroupData {
	index := map[string]int{}
	var groups []notifGroupData
	for _, n := range notifs {
		i, ok := index[n.AppName]
		if !ok {
			i = len(groups)
			index[n.AppName] = i
			groups = append(groups, notifGroupData{app: n.AppName})
		}
		groups[i].notifs = append(groups[i].notifs, n)
	}
	newestFirst := func(a, b *notifications.Notification) int { return b.Added.Compare(a.Added) }
	for i := range groups {
		slices.SortStableFunc(groups[i].notifs, newestFirst)
	}
	slices.SortStableFunc(groups, func(a, b notifGroupData) int {
		return newestFirst(a.notifs[0], b.notifs[0])
	})
	return groups
}

// notifTimeLabel is time_to_string.
func notifTimeLabel(age notifyui.Age) string { return notifyui.TimeLabel(age, "notification-dropdown") }

// notificationView is the notification dropdown: the header (bell or
// muted bell, clear all), the do-not-disturb row, and the app groups or
// the empty state, following the service while open.
type notificationView struct {
	ctx  ModuleContext
	svc  *notifications.Service
	font render.Font
	px   float64
	now  func() time.Time
	popoverHook

	*widget.Box
	headerIcon, emptyIcon *widget.Icon
	clearAll              *widget.Button
	dnd                   *widget.Switch
	syncing               bool
	body                  *widget.Stack
	list                  *widget.Box
	groups                []*notifGroup

	once sync.Once
	stop chan struct{}
}

func notificationDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &notificationView{ctx: ctx, svc: ctx.Notifications, font: font, px: px, now: time.Now, stop: make(chan struct{})}
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "notification-dropdown")

	clearLabel := widget.NewLabel(font, px*0.9, i18n.T("notification-dropdown-clear-all"), ctx.Style.fg)
	v.clearAll = dropdownButton(ctx, clearLabel, "notification-dropdown-clear-all", v.dismissAll)
	header, headerIcon := dropdownHeaderIcon(ctx, font, px, "ld-bell-symbolic", i18n.T("notification-dropdown-title"), v.clearAll)
	v.headerIcon = headerIcon
	v.Append(header, false)

	dndRow := widget.NewBox(widget.Row, 8, 0)
	dndRow.AddClass("notification-dropdown-dnd-row")
	dndLabel := widget.NewLabel(font, px, i18n.T("notification-dropdown-dnd-label"), ctx.Style.fg)
	dndLabel.AddClass("notification-dropdown-dnd-label")
	dndRow.Append(dndLabel, true)
	v.dnd = widget.NewSwitch(false)
	v.dnd.OnChanged = v.dndToggled
	dndRow.Append(v.dnd, false)
	v.Append(dndRow, false)

	empty, emptyIcon := emptyStateIcon(ctx, font, px, "ld-bell-symbolic",
		i18n.T("notification-dropdown-empty-title"), i18n.T("notification-dropdown-empty-description"))
	v.emptyIcon = emptyIcon
	v.list = widget.NewBox(widget.Column, 8, 0)
	v.list.AddClass("notification-dropdown-groups")
	scroll := dropdownScroll(v.list, "notification-dropdown-scroll")
	v.body = widget.NewStack()
	v.body.AddClass("notification-dropdown-content")
	v.body.Add("empty", empty)
	v.body.Add("list", scroll)
	v.Append(v.body, true)

	v.syncDND()
	v.rebuild()
	v.follow()
	return v
}

// syncDND is DndChanged: the switch (without writing back) and the
// bell icons.
func (v *notificationView) syncDND() {
	on := v.svc != nil && v.svc.DND()
	v.syncing = true
	v.dnd.SetOn(on)
	v.syncing = false
	icon := "ld-bell-symbolic"
	if on {
		icon = "ld-bell-off-symbolic"
	}
	v.headerIcon.SetThemeName(icon)
	v.emptyIcon.SetThemeName(icon)
}

func (v *notificationView) dndToggled(on bool) {
	if v.syncing || v.svc == nil {
		return
	}
	v.svc.SetDND(on)
}

func (v *notificationView) dismissAll() {
	if v.svc != nil {
		v.svc.DismissAll()
	}
}

// dismiss is Notification::dismiss.
func (v *notificationView) dismiss(id uint32) {
	v.svc.Close(id, notifications.Dismissed)
}

// rebuild is rebuild_groups: stale groups go, existing ones take their
// new notifications (keeping their expansion), new ones are added, and
// the list takes the newest-first order.
func (v *notificationView) rebuild() {
	var notifs []*notifications.Notification
	if v.svc != nil {
		notifs = v.svc.Notifications()
	}
	v.clearAll.SetVisible(len(notifs) > 0)
	if len(notifs) == 0 {
		v.body.Show("empty")
	} else {
		v.body.Show("list")
	}
	byApp := make(map[string]*notifGroup, len(v.groups))
	for _, g := range v.groups {
		byApp[g.app] = g
	}
	next := make([]*notifGroup, 0, len(v.groups))
	for _, data := range groupByApp(notifs) {
		g, ok := byApp[data.app]
		if ok {
			g.update(data.notifs)
		} else {
			g = newNotifGroup(v, data)
		}
		next = append(next, g)
	}
	v.groups = next
	v.list.Clear()
	for _, g := range next {
		v.list.Append(g, false)
	}
}

// refreshTimes is TimeTick.
func (v *notificationView) refreshTimes() {
	for _, g := range v.groups {
		for _, it := range g.items {
			it.refreshTime()
		}
	}
}

// follow rebuilds on history changes, syncs DND, and refreshes the
// relative times every 30s, until the dropdown closes.
func (v *notificationView) follow() {
	if v.svc == nil {
		return
	}
	events, stop := v.svc.Subscribe()
	go func() {
		defer stop()
		tick := time.NewTicker(notifTimeTick)
		defer tick.Stop()
		for {
			select {
			case <-v.stop:
				return
			case <-tick.C:
				v.ctx.Invoke(v.refreshTimes)
			case ev, ok := <-events:
				if !ok {
					return
				}
				switch ev.Kind {
				case notifications.EventDnd:
					v.ctx.Invoke(v.syncDND)
				case notifications.EventAdd, notifications.EventRemove:
					v.ctx.Invoke(v.rebuild)
				}
			}
		}
	}()
}

// dropdownClosed implements dropdownCloser.
func (v *notificationView) dropdownClosed() { v.once.Do(func() { close(v.stop) }) }

// notifGroup is NotificationGroup: the header (icon, app name, count,
// collapsed preview, clear, chevron), the visible items, and the "N
// more" row.
type notifGroup struct {
	*widget.Box
	view     *notificationView
	app      string
	expanded bool
	notifs   []*notifications.Notification
	items    []*notifItem
	overflow int

	icon           *widget.Icon
	count, preview *widget.Label
	chevron        *widget.Icon
	itemsBox, list *widget.Box
	more           *widget.Button
	moreLabel      *widget.Label
}

func newNotifGroup(v *notificationView, data notifGroupData) *notifGroup {
	g := &notifGroup{view: v, app: data.app, expanded: true, Box: widget.NewBox(widget.Column, 6, 0)}
	g.AddClass("notification-dropdown-group")
	fg, muted := v.ctx.Style.fg, mutedFg(v.ctx.Style.palette)

	g.icon = widget.NewThemeIcon(notifyui.FallbackIcon, int(v.px*1.3))
	g.icon.SetTint(fg)
	g.icon.AddClass("notification-dropdown-group-icon")
	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("notification-dropdown-group-info")
	nameRow := widget.NewBox(widget.Row, 4, 0)
	appName := data.app
	if appName == "" {
		appName = i18n.T("notification-dropdown-unknown-app")
	}
	name := widget.NewLabel(v.font, v.px, appName, fg)
	name.AddClass("notification-dropdown-group-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	nameRow.Append(name, false)
	g.count = widget.NewLabel(v.font, v.px*0.9, "", muted)
	g.count.AddClass("notification-dropdown-group-count")
	nameRow.Append(g.count, false)
	info.Append(nameRow, false)
	g.preview = widget.NewLabel(v.font, v.px*0.85, "", muted)
	g.preview.AddClass("notification-dropdown-group-preview")
	g.preview.SetEllipsize(widget.EllipsizeEnd)
	info.Append(g.preview, false)
	toggleRow := widget.NewBox(widget.Row, 8, 0)
	toggleRow.Append(g.icon, false)
	toggleRow.Append(info, true)

	header := widget.NewBox(widget.Row, 4, 0)
	header.AddClass("notification-dropdown-group-header")
	header.Append(dropdownButton(v.ctx, toggleRow, "notification-dropdown-group-toggle", g.toggle), true)
	clearLabel := widget.NewLabel(v.font, v.px*0.85, i18n.T("notification-dropdown-group-clear"), muted)
	header.Append(dropdownButton(v.ctx, clearLabel, "notification-dropdown-group-clear", g.clear), false)
	g.chevron = widget.NewThemeIcon("ld-chevron-up-symbolic", int(v.px))
	g.chevron.SetTint(muted)
	g.chevron.AddClass("notification-dropdown-group-chevron-icon")
	header.Append(dropdownButton(v.ctx, g.chevron, "notification-dropdown-group-chevron", g.toggle), false)
	g.Append(header, false)

	g.itemsBox = widget.NewBox(widget.Column, 6, 0)
	g.itemsBox.AddClass("notification-dropdown-group-items")
	g.list = widget.NewBox(widget.Column, 6, 0)
	g.itemsBox.Append(g.list, false)
	g.moreLabel = widget.NewLabel(v.font, v.px*0.85, "", tokenColor(v.ctx.Style.palette, config.TokenAccent))
	g.more = dropdownButton(v.ctx, g.moreLabel, "notification-dropdown-group-more", g.showAll)
	g.itemsBox.Append(g.more, false)
	g.Append(g.itemsBox, false)

	g.update(data.notifs)
	return g
}

// update is UpdateNotifications: the group icon (always the mapped one
// of the newest) and reconcile_items.
func (g *notifGroup) update(notifs []*notifications.Notification) {
	g.icon.SetThemeName(notifyui.ResolveIcon(config.IconSourceMapped, notifs[0]).Name)
	g.notifs = notifs
	g.count.SetText("(" + strconv.Itoa(len(notifs)) + ")")
	g.count.SetVisible(len(notifs) > 1)
	g.preview.SetText(notifs[0].Summary)
	limit := notifMaxVisibleItems
	if len(g.items) > notifMaxVisibleItems {
		limit = len(notifs)
	}
	visible := notifs[:min(len(notifs), limit)]
	g.overflow = len(notifs) - len(visible)
	same := slices.EqualFunc(g.items, visible, func(it *notifItem, n *notifications.Notification) bool {
		return it.n.ID == n.ID
	})
	if !same {
		g.items = g.items[:0]
		g.list.Clear()
		g.appendItems(visible)
	}
	g.sync()
}

func (g *notifGroup) appendItems(notifs []*notifications.Notification) {
	for _, n := range notifs {
		it := newNotifItem(g.view, n)
		g.items = append(g.items, it)
		g.list.Append(it, false)
	}
}

// sync applies the #[watch]es on expanded and overflow.
func (g *notifGroup) sync() {
	g.itemsBox.SetVisible(g.expanded)
	g.preview.SetVisible(!g.expanded)
	if g.expanded {
		g.chevron.SetThemeName("ld-chevron-up-symbolic")
	} else {
		g.chevron.SetThemeName("ld-chevron-down-symbolic")
	}
	g.moreLabel.SetText(i18n.T("notification-dropdown-group-more", i18n.Str("count", strconv.Itoa(g.overflow))))
	g.more.SetVisible(g.overflow > 0)
}

// toggle is ToggleExpanded: collapsing drops back to the default cap
// (reset_to_default_cap).
func (g *notifGroup) toggle() {
	g.expanded = !g.expanded
	if !g.expanded && len(g.items) > notifMaxVisibleItems {
		g.items = g.items[:notifMaxVisibleItems]
		g.list.Clear()
		for _, it := range g.items {
			g.list.Append(it, false)
		}
		g.overflow = len(g.notifs) - notifMaxVisibleItems
	}
	g.sync()
}

// showAll is show_all_items.
func (g *notifGroup) showAll() {
	g.appendItems(g.notifs[len(g.items):])
	g.overflow = 0
	g.sync()
}

// clear is ClearGroup.
func (g *notifGroup) clear() {
	for _, n := range g.notifs {
		g.view.dismiss(n.ID)
	}
}

// notifItem is NotificationItem: the icon, the summary with its
// relative time and dismiss button, the two-line body, and the
// non-default actions in rows of three. A default action makes the
// main row a button that closes the dropdown and invokes it.
type notifItem struct {
	*widget.Box
	view *notificationView
	n    *notifications.Notification
	time *widget.Label
}

func newNotifItem(v *notificationView, n *notifications.Notification) *notifItem {
	it := &notifItem{view: v, n: n, Box: widget.NewBox(widget.Column, 6, 8)}
	it.AddClass("notification-dropdown-item", notifyui.UrgencyClass(n.Urgency))
	fg, muted := v.ctx.Style.fg, mutedFg(v.ctx.Style.palette)

	main := widget.NewBox(widget.Row, 10, 0)
	main.AddClass("notification-dropdown-item-main")
	icon := notifyui.ResolveIcon(v.ctx.Config.Notification.IconSource, n)
	glyph := notifyui.NewIcon(icon, notifIconPx, fg)
	glyph.AddClass("notification-dropdown-item-icon")
	if notifyui.IsFileIcon(icon) {
		glyph.AddClass("file-icon")
	}
	main.Append(glyph, false)

	content := widget.NewBox(widget.Column, 2, 0)
	content.AddClass("notification-dropdown-item-content")
	header := widget.NewBox(widget.Row, 6, 0)
	header.AddClass("notification-dropdown-item-header")
	title := widget.NewLabel(v.font, v.px, n.Summary, fg)
	title.AddClass("notification-dropdown-item-title")
	title.SetEllipsize(widget.EllipsizeEnd)
	header.Append(title, true)
	it.time = widget.NewLabel(v.font, v.px*0.8, "", muted)
	it.time.AddClass("notification-dropdown-item-time")
	header.Append(it.time, false)
	x := widget.NewThemeIcon("ld-x-symbolic", int(v.px*0.9))
	x.SetTint(muted)
	id := n.ID
	header.Append(dropdownButton(v.ctx, x, "notification-dropdown-item-dismiss", func() { v.dismiss(id) }), false)
	content.Append(header, false)
	if n.Body != "" {
		body := widget.NewLabel(v.font, v.px*0.9, notifyui.BodyText(n.Body), muted)
		body.AddClass("notification-dropdown-item-body")
		body.SetWrap(true)
		body.SetEllipsize(widget.EllipsizeEnd)
		body.SetMaxLines(2)
		content.Append(body, false)
	}
	main.Append(content, true)

	// A default action makes the row open on a click anywhere the
	// dismiss button does not take (setup_default_action's gesture).
	if _, ok := n.DefaultAction(); ok {
		main.AddClass("notification-dropdown-item-default")
		main.SetOnClickWithin(it.invokeDefault)
	}
	it.Append(main, false)
	if actions := it.actionRows(); actions != nil {
		it.Append(actions, false)
	}
	it.refreshTime()
	return it
}

// actionRows is build_action_buttons; nil without visible actions.
func (it *notifItem) actionRows() widget.Widget {
	visible := notifyui.VisibleActions(it.n)
	if len(visible) == 0 {
		return nil
	}
	v := it.view
	box := widget.NewBox(widget.Column, 4, 0)
	box.AddClass("notification-dropdown-item-actions")
	for chunk := range slices.Chunk(visible, notifyui.ActionsPerRow) {
		row := widget.NewBox(widget.Row, 4, 0)
		row.AddClass("notification-dropdown-item-action-row")
		for _, a := range chunk {
			label := widget.NewLabel(v.font, v.px*0.9, a.Label, v.ctx.Style.fg)
			label.SetAlignment(render.AlignCenter)
			key := a.ID
			row.Append(dropdownButton(v.ctx, label, "notification-dropdown-item-action-btn", func() { it.invoke(key) }), true)
		}
		box.Append(row, false)
	}
	return box
}

// invoke runs an action and dismisses the notification, as the Rust
// button does (invoke, then dismiss, resident or not).
func (it *notifItem) invoke(key string) {
	svc := it.view.svc
	svc.InvokeAction(it.n.ID, key)
	svc.Close(it.n.ID, notifications.Dismissed)
}

// invokeDefault closes the dropdown first: the default action usually
// raises another window (setup_default_action).
func (it *notifItem) invokeDefault() {
	it.view.popdown()
	it.invoke(notifications.DefaultActionID)
}

// refreshTime is RefreshTime.
func (it *notifItem) refreshTime() {
	it.time.SetText(notifTimeLabel(notifyui.RelativeTime(it.view.now(), it.n.Added)))
}
