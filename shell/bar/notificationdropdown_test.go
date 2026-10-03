package bar

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
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
	if !v.empty.Visible() || v.scroll.Visible() || v.clearAll.Visible() {
		t.Errorf("no service: empty shown %v, list shown %v, clear all shown %v", v.empty.Visible(), v.scroll.Visible(), v.clearAll.Visible())
	}

	svc := newNotifTestService(t)
	ctx.Notifications = svc
	v = notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()
	if !v.empty.Visible() || v.scroll.Visible() || v.headerIcon.Name() != "ld-bell-symbolic" {
		t.Errorf("empty service: empty shown %v, list shown %v, icon %q", v.empty.Visible(), v.scroll.Visible(), v.headerIcon.Name())
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

	if !v.scroll.Visible() || v.empty.Visible() || !v.clearAll.Visible() || len(v.groups) != 2 {
		t.Fatalf("list shown %v, empty shown %v, %d groups", v.scroll.Visible(), v.empty.Visible(), len(v.groups))
	}
	chatGroup, mail := v.groups[0], v.groups[1]
	if chatGroup.app != "chat" || mail.app != "mail" {
		t.Fatalf("groups = %q, %q; want the newest app first", chatGroup.app, mail.app)
	}
	if chatGroup.count.Visible() || !mail.count.Visible() || mail.count.Text() != "(5)" {
		t.Errorf("counts: chat shown %v, mail %q", chatGroup.count.Visible(), mail.count.Text())
	}
	if len(mail.items) != notifMaxVisibleItems || mail.overflow != 2 || !mail.moreLabel.Visible() {
		t.Errorf("mail: %d items, overflow %d", len(mail.items), mail.overflow)
	}
	if mail.moreLabel.Text() != i18n.T("notification-dropdown-group-more", i18n.Str("count", "2")) {
		t.Errorf("more = %q", mail.moreLabel.Text())
	}

	// Show all, then collapse back to the cap.
	mail.showAll()
	if len(mail.items) != 5 || mail.moreLabel.Visible() {
		t.Errorf("show all: %d items, more shown %v", len(mail.items), mail.moreLabel.Visible())
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
	if rows := actions.Children(); len(rows) != 1 || len(rows[0].(*widget.Grid).Children()) != 1 {
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
	waitHeadless(t, "the empty state", func() bool { return v.empty.Visible() && !v.scroll.Visible() })
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
	body := content.Children()[1].(*widget.RichLabel)
	if body.Text() != "a & b" || body.MaxLines() != 2 || !body.Wrap() {
		t.Errorf("body = %q (max lines %d, wrap %v)", body.Text(), body.MaxLines(), body.Wrap())
	}
	plainContent := plain.Children()[0].(*widget.Box).Children()[1].(*widget.Box)
	if len(plainContent.Children()) != 1 {
		t.Error("an empty body still shows a label")
	}
	tile := ff.Children()[0].(*widget.Box).Children()[0].(*widget.Box)
	icon := tile.Children()[0].(*widget.Icon)
	if icon.Name() != "si-firefox-symbolic" || !icon.HasClass("notification-dropdown-item-icon-img") {
		t.Errorf("mapped icon = %q", icon.Name())
	}
	if !tile.HasClass("notification-dropdown-item-icon") || tile.HasClass("file-icon") {
		t.Error("the mapped icon tile classes")
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

// TestNotificationGroupHeaderClickTargets pins the header's click
// split: the whole header box toggles the group (the GestureClick),
// the clear button inside it dismisses without toggling, and the
// header and the more row carry the pointer.
func TestNotificationGroupHeaderClickTargets(t *testing.T) {
	svc := newNotifTestService(t)
	for range 5 {
		svc.Notify("mail", 0, "", "Mail", "", nil, 0)
	}
	ctx := newTestContext(t, config.Defaults())
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()
	g := v.groups[0]
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: 425, H: 725}})
	v.Arrange(render.Rect{W: sz.W, H: sz.H})

	header := findByClass(g, "notification-dropdown-group-header").(*widget.Box)
	if widget.CursorNameOf(header) != "pointer" {
		t.Error("the header does not ask for the pointer")
	}
	routeClick(v, header)
	if g.expanded {
		t.Error("a header click did not collapse the group")
	}
	routeClick(v, header)
	if !g.expanded {
		t.Error("a second header click did not re-expand the group")
	}

	// The clear button's own click dismisses the group and never
	// bubbles to the header's toggle.
	routeClick(v, findByClass(g, "notification-dropdown-group-clear"))
	waitHeadless(t, "the cleared group", func() bool { return len(v.groups) == 0 })
	if !g.expanded {
		t.Error("the clear button's click toggled the group")
	}
}

// TestNotificationGroupIconClasses pins the icon class split: the tile
// box carries -group-icon, the image inside it only -group-icon-img
// (the image must not carry the box class, and the tile not the img
// class).
func TestNotificationGroupIconClasses(t *testing.T) {
	svc := newNotifTestService(t)
	svc.Notify("mail", 0, "", "Mail", "", nil, 0)
	ctx := newTestContext(t, config.Defaults())
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()

	g := v.groups[0]
	tile := findByClass(g, "notification-dropdown-group-icon").(*widget.Box)
	img := tile.Children()[0].(*widget.Icon)
	if !img.HasClass("notification-dropdown-group-icon-img") {
		t.Error("the group image misses -group-icon-img")
	}
	if img.HasClass("notification-dropdown-group-icon") {
		t.Error("the group image carries the tile's -group-icon class")
	}
	if tile.HasClass("notification-dropdown-group-icon-img") {
		t.Error("the tile carries the image's -group-icon-img class")
	}
}

// TestNotificationClearAllStructure pins GhostButton's tree: the
// Button wraps a box around the label, so the stylesheet's
// button.notification-dropdown-clear-all > box margin applies — the
// arranged box sits strictly inside the button — and the class stays
// on the button.
func TestNotificationClearAllStructure(t *testing.T) {
	svc := newNotifTestService(t)
	svc.Notify("mail", 0, "", "Mail", "", nil, 0)
	ctx := styledContext(t, config.Defaults())
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()
	ctx.Theme.Attach(v)
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: 425, H: 725}})
	v.Arrange(render.Rect{W: sz.W, H: sz.H})

	lbl, ok := v.clearBox.Children()[0].(*widget.Label)
	if !ok || lbl.Text() != i18n.T("notification-dropdown-clear-all") {
		t.Fatalf("the clear-all button wraps %T, want the label in a box", v.clearBox.Children()[0])
	}
	if v.clearBox.HasClass("notification-dropdown-clear-all") {
		t.Error("the inner box stole the button's class")
	}
	btn, box := v.clearAll.Bounds(), v.clearBox.Bounds()
	if box.W >= btn.W || box.X <= btn.X {
		t.Errorf("the inner box fills the button (box %v, button %v): the > box margin did not apply", box, btn)
	}
}

// TestNotificationMoreRowIsALabel pins the more row: the class sits on
// the label itself, the label carries the click and the pointer, and a
// click shows the hidden items.
func TestNotificationMoreRowIsALabel(t *testing.T) {
	svc := newNotifTestService(t)
	for range 5 {
		svc.Notify("mail", 0, "", "Mail", "", nil, 0)
	}
	ctx := newTestContext(t, config.Defaults())
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()
	g := v.groups[0]
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: 425, H: 725}})
	v.Arrange(render.Rect{W: sz.W, H: sz.H})

	more, ok := findByClass(g, "notification-dropdown-group-more").(*widget.Label)
	if !ok {
		t.Fatal("the more row is not the label itself")
	}
	if more != g.moreLabel || !more.Visible() {
		t.Error("the overflow more row is not the group's label")
	}
	if widget.CursorNameOf(more) != "pointer" {
		t.Error("the more row does not ask for the pointer")
	}
	routeClick(v, more)
	if len(g.items) != 5 || g.overflow != 0 || more.Visible() {
		t.Errorf("the more click did not show all: %d items, overflow %d", len(g.items), g.overflow)
	}
}

// TestNotificationItemCursorAndSpacing pins the zero additive spacing
// (the stylesheet's border-spacing carries the gaps) and the pointer
// on a default-action row — and no pointer without one.
func TestNotificationItemCursorAndSpacing(t *testing.T) {
	svc := newNotifTestService(t)
	svc.Notify("chat", 0, "", "Ping", "", []string{"default", "Open", "reply", "Reply"}, 0)
	svc.Notify("plain", 0, "", "Quiet", "", nil, 0)
	ctx := newTestContext(t, config.Defaults())
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()

	var chat, plain *notifItem
	for _, g := range v.groups {
		switch g.app {
		case "chat":
			chat = g.items[0]
		case "plain":
			plain = g.items[0]
		}
	}
	if widget.CursorNameOf(chat.Children()[0]) != "pointer" {
		t.Error("the default-action row does not ask for the pointer")
	}
	if widget.CursorNameOf(plain.Children()[0]) != "" {
		t.Error("a row without a default action asks for the pointer")
	}

	main := chat.Children()[0].(*widget.Box)
	content := main.Children()[1].(*widget.Box)
	actions, ok := chat.Children()[1].(*widget.Box).Children()[0].(*widget.Grid)
	if !ok {
		t.Fatal("the action row is not a homogeneous grid")
	}
	for name, b := range map[string]*widget.Box{
		"root": v.Box, "item": chat.Box, "main": main, "content": content,
		"group": v.groups[1].Box, "items": v.groups[1].itemsBox, "list": v.groups[1].list,
	} {
		if b.Spacing() != 0 || b.Padding() != (render.Insets{}) {
			t.Errorf("%s carries additive spacing %d or padding %v", name, b.Spacing(), b.Padding())
		}
	}
	if !actions.ColumnHomogeneous() || actions.ColumnSpacing() != 0 {
		t.Errorf("the action row = homogeneous %v, spacing %d", actions.ColumnHomogeneous(), actions.ColumnSpacing())
	}
	if cols := actions.Children(); len(cols) != 1 {
		t.Errorf("the action row holds %d buttons, want Reply", len(cols))
	}
}

// TestNotificationItemFileIconFallsBack pins apply_icon's load
// failure: an unloadable file picture shows the themed bell and its
// tile never takes the file-icon class; a loadable one keeps the file
// icon and the class.
func TestNotificationItemFileIconFallsBack(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone.png")
	kept := filepath.Join(t.TempDir(), "kept.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("png: %v", err)
	}
	if err := os.WriteFile(kept, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	svc := newNotifTestService(t)
	svc.NotifyHints("a", 0, "", "Gone", "", nil, map[string]dbus.Variant{"image-path": dbus.MakeVariant(gone)}, 0)
	svc.NotifyHints("b", 0, "", "Kept", "", nil, map[string]dbus.Variant{"image-path": dbus.MakeVariant(kept)}, 0)
	cfg := config.Defaults()
	cfg.Notification.IconSource = config.IconSourceAutomatic
	ctx := newTestContext(t, cfg)
	ctx.Notifications = svc
	v := notificationDropdown(ctx).(*notificationView)
	defer v.dropdownClosed()

	var missing, loaded *notifItem
	for _, g := range v.groups {
		switch g.app {
		case "a":
			missing = g.items[0]
		case "b":
			loaded = g.items[0]
		}
	}
	missTile := missing.Children()[0].(*widget.Box).Children()[0].(*widget.Box)
	missIcon := missTile.Children()[0].(*widget.Icon)
	if missIcon.Name() != notifyui.FallbackIcon {
		t.Errorf("the unloadable file icon = %q, want the bell", missIcon.Name())
	}
	if missTile.HasClass("file-icon") {
		t.Error("the bell tile took the file-icon class")
	}
	keepTile := loaded.Children()[0].(*widget.Box).Children()[0].(*widget.Box)
	keepIcon := keepTile.Children()[0].(*widget.Icon)
	if keepIcon.Name() != "" || !keepTile.HasClass("file-icon") {
		t.Errorf("the loadable file lost its icon (name %q, file-icon %v)", keepIcon.Name(), keepTile.HasClass("file-icon"))
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
