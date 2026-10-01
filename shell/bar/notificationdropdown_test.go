package bar

import (
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/shell/notifyui"
)

func TestGroupByApp(t *testing.T) {
	base := time.Unix(1000, 0)
	n := func(id uint32, app string, at int) *notifications.Notification {
		return &notifications.Notification{ID: id, AppName: app, Added: base.Add(time.Duration(at) * time.Second)}
	}
	groups := groupByApp([]*notifications.Notification{n(1, "mail", 1), n(2, "chat", 5), n(3, "mail", 9), n(4, "", 3)})
	var apps []string
	for _, g := range groups {
		apps = append(apps, g.app)
	}
	if len(apps) != 3 || apps[0] != "mail" || apps[1] != "chat" || apps[2] != "" {
		t.Fatalf("group order = %q, want by newest notification", apps)
	}
	if ids := groups[0].notifs; ids[0].ID != 3 || ids[1].ID != 1 {
		t.Errorf("mail group = %v, want newest first", ids)
	}
	if len(groupByApp(nil)) != 0 {
		t.Error("no notifications made groups")
	}
}

func TestNotifTimeLabel(t *testing.T) {
	if got := notifTimeLabel(notifyui.Age{}); got != i18n.T("notification-dropdown-time-just-now") {
		t.Errorf("just now = %q", got)
	}
	if got := notifTimeLabel(notifyui.Age{Minutes: 5}); got != i18n.T("notification-dropdown-time-minutes-ago", i18n.Str("minutes", "5")) {
		t.Errorf("minutes = %q", got)
	}
	if got := notifTimeLabel(notifyui.Age{Hours: 3}); got != i18n.T("notification-dropdown-time-hours-ago", i18n.Str("hours", "3")) {
		t.Errorf("hours = %q", got)
	}
}

func newNotifTestService(t *testing.T) *notifications.Service {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	svc := notifications.NewService()
	svc.SetDND(false)
	return svc
}

func TestNotificationDropdownEmptyAndDND(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := notificationDropdown(ctx).(*notificationView)
	if v.body.Visible() != "empty" || v.clearAll.Visible() {
		t.Errorf("no service: page %q, clear all shown %v", v.body.Visible(), v.clearAll.Visible())
	}

	svc := newNotifTestService(t)
	ctx.Notifications = svc
	v = notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()
	if v.body.Visible() != "empty" || v.headerIcon.Name() != "ld-bell-symbolic" {
		t.Errorf("empty service: page %q icon %q", v.body.Visible(), v.headerIcon.Name())
	}
	// The switch writes DND; the service's change comes back as the
	// muted bells without writing again.
	v.dnd.SetOn(true)
	if !svc.DND() {
		t.Fatal("the switch did not enable DND")
	}
	waitHeadless(t, "the muted bells", func() bool {
		return v.headerIcon.Name() == "ld-bell-off-symbolic" && v.emptyIcon.Name() == "ld-bell-off-symbolic"
	})
	// A service-side change moves the switch.
	svc.SetDND(false)
	waitHeadless(t, "the switch off", func() bool { return !v.dnd.On() })
	if svc.DND() {
		t.Error("syncing the switch wrote DND back")
	}
}

func TestNotificationDropdownGroups(t *testing.T) {
	svc := newNotifTestService(t)
	for range 5 {
		svc.Notify("mail", 0, "", "Mail", "<b>body</b> text", nil, 0)
	}
	chat := svc.Notify("chat", 0, "", "Ping", "", []string{"reply", "Reply", "default", "Open"}, 0)
	ctx := newTestContext(t, config.Defaults())
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()

	if v.body.Visible() != "list" || !v.clearAll.Visible() || len(v.groups) != 2 {
		t.Fatalf("page %q, %d groups", v.body.Visible(), len(v.groups))
	}
	chatGroup, mail := v.groups[0], v.groups[1]
	if chatGroup.app != "chat" || mail.app != "mail" {
		t.Fatalf("groups = %q, %q; want the newest app first", chatGroup.app, mail.app)
	}
	if chatGroup.count.Visible() || !mail.count.Visible() || mail.count.Text() != "(5)" {
		t.Errorf("counts: chat shown %v, mail %q", chatGroup.count.Visible(), mail.count.Text())
	}
	if len(mail.items) != notifMaxVisibleItems || mail.overflow != 2 || !mail.more.Visible() {
		t.Errorf("mail: %d items, overflow %d", len(mail.items), mail.overflow)
	}
	if mail.moreLabel.Text() != i18n.T("notification-dropdown-group-more", i18n.Str("count", "2")) {
		t.Errorf("more = %q", mail.moreLabel.Text())
	}

	// Show all, then collapse back to the cap.
	mail.showAll()
	if len(mail.items) != 5 || mail.more.Visible() {
		t.Errorf("show all: %d items, more shown %v", len(mail.items), mail.more.Visible())
	}
	mail.toggle()
	if mail.expanded || mail.itemsBox.Visible() || !mail.preview.Visible() || len(mail.items) != notifMaxVisibleItems || mail.overflow != 2 {
		t.Errorf("collapse: expanded %v, %d items, overflow %d", mail.expanded, len(mail.items), mail.overflow)
	}
	mail.toggle()

	// A new mail updates the group in place, keeping its widget, and
	// moves it to the top.
	svc.Notify("mail", 0, "", "Newest", "", nil, 0)
	waitHeadless(t, "the regroup", func() bool { return len(v.groups) == 2 && v.groups[0] == mail })
	if mail.items[0].n.Summary != "Newest" || mail.count.Text() != "(6)" {
		t.Errorf("updated mail group: first %q count %q", mail.items[0].n.Summary, mail.count.Text())
	}

	// The chat item: a default action row and one visible action.
	item := chatGroup.items[0]
	main := item.Children()[0].(*widget.Box)
	if !main.HasClass("notification-dropdown-item-default") {
		t.Error("the default action did not mark the row")
	}
	actions := item.Children()[1].(*widget.Box)
	if rows := actions.Children(); len(rows) != 1 || len(rows[0].(*widget.Box).Children()) != 1 {
		t.Errorf("action rows = %v, want one row with Reply", rows)
	}
	pop := &fakePopover{}
	v.attachPopover(pop)
	item.Measure(widget.Constraints{Max: widget.Size{W: 360, H: 400}})
	item.Arrange(render.Rect{W: 360, H: 400})
	routeClick(item, findByClass(main, "notification-dropdown-item-title"))
	if pop.dismissed != 1 {
		t.Error("the default action did not close the dropdown")
	}
	waitHeadless(t, "the chat group gone", func() bool { return len(v.groups) == 1 })
	for _, n := range svc.Notifications() {
		if n.ID == chat {
			t.Error("the invoked notification stayed")
		}
	}

	// Clearing the group empties the dropdown.
	mail.clear()
	waitHeadless(t, "the empty state", func() bool { return v.body.Visible() == "empty" })
	if v.clearAll.Visible() {
		t.Error("clear all stays without notifications")
	}
}

func TestNotificationItemBodyIconAndUrgency(t *testing.T) {
	svc := newNotifTestService(t)
	svc.NotifyHints("firefox", 0, "", "Download", "a &amp; <b>b</b>", nil,
		map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(2))}, 0)
	svc.Notify("plain", 0, "", "No body", "", nil, 0)
	cfg := config.Defaults()
	cfg.Notification.IconSource = config.IconSourceMapped
	ctx := newTestContext(t, cfg)
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()

	var ff, plain *notifItem
	for _, g := range v.groups {
		switch g.app {
		case "firefox":
			ff = g.items[0]
		case "plain":
			plain = g.items[0]
		}
	}
	if !ff.HasClass("critical") || plain.HasClass("critical") || !plain.HasClass("normal") {
		t.Error("urgency classes")
	}
	content := ff.Children()[0].(*widget.Box).Children()[1].(*widget.Box)
	body := content.Children()[1].(*widget.Label)
	if body.Text() != "a & b" || body.MaxLines() != 2 {
		t.Errorf("body = %q (max lines %d)", body.Text(), body.MaxLines())
	}
	plainContent := plain.Children()[0].(*widget.Box).Children()[1].(*widget.Box)
	if len(plainContent.Children()) != 1 {
		t.Error("an empty body still shows a label")
	}
	icon := ff.Children()[0].(*widget.Box).Children()[0].(*widget.Icon)
	if icon.Name() != "si-firefox-symbolic" || icon.HasClass("file-icon") {
		t.Errorf("mapped icon = %q", icon.Name())
	}
	if ff.time.Text() != i18n.T("notification-dropdown-time-just-now") {
		t.Errorf("time = %q", ff.time.Text())
	}
	v.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	v.refreshTimes()
	if ff.time.Text() != i18n.T("notification-dropdown-time-hours-ago", i18n.Str("hours", "2")) {
		t.Errorf("refreshed time = %q", ff.time.Text())
	}
}

// The sized panel forwards the popover handle to content that acts on
// its popover, and ignores content that does not.
func TestPanelBoxForwardsThePopover(t *testing.T) {
	inner := &notificationView{Box: widget.NewBox(widget.Column, 0, 0)}
	pop := &fakePopover{}
	newPanelBox(10, 10, inner).attachPopover(pop)
	inner.popdown()
	entry := widget.NewEntry(testFont(t), 12, 0)
	inner.focus(entry)
	if pop.dismissed != 1 || pop.focused != entry {
		t.Errorf("forwarded: dismissed %d focused %v", pop.dismissed, pop.focused)
	}
	newPanelBox(10, 10, widget.NewBox(widget.Column, 0, 0)).attachPopover(pop) // ignored
	var unset popoverHook
	unset.popdown() // no popover yet: a no-op
	// A focus asked for before the popover exists lands on attach.
	unset.focus(entry)
	late := &fakePopover{}
	unset.attachPopover(late)
	if late.focused != entry {
		t.Error("the pending focus was lost")
	}
}

// TestNotificationItemDismissInsideADefaultActionRow pins the row's
// click split: the dismiss button inside a row with a default action
// dismisses, without invoking the action or closing the dropdown.
// Regression: the row was a button, a hit leaf, so its dismiss button
// could never be clicked.
func TestNotificationItemDismissInsideADefaultActionRow(t *testing.T) {
	svc := newNotifTestService(t)
	svc.Notify("chat", 0, "", "Ping", "", []string{"default", "Open"}, 0)
	var invoked []string
	svc.SetEmitter(func(member string, args ...any) {
		if strings.HasSuffix(member, ".ActionInvoked") {
			invoked = append(invoked, member)
		}
	})
	ctx := newTestContext(t, config.Defaults())
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()
	pop := &fakePopover{}
	v.attachPopover(pop)
	item := v.groups[0].items[0]
	item.Measure(widget.Constraints{Max: widget.Size{W: 360, H: 400}})
	item.Arrange(render.Rect{W: 360, H: 400})
	routeClick(item, findByClass(item, "notification-dropdown-item-dismiss"))
	waitHeadless(t, "the dismissal", func() bool { return len(svc.Notifications()) == 0 })
	if pop.dismissed != 0 || len(invoked) != 0 {
		t.Errorf("dismiss closed the dropdown (%d) or invoked %v", pop.dismissed, invoked)
	}
}
