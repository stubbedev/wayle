package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/notifications"
)

func TestNotificationLabel(t *testing.T) {
	// helpers.rs: zero-padded to two digits, three digits for 100.
	for _, tc := range []struct {
		count int
		want  string
	}{
		{0, "00"}, {5, "05"}, {12, "12"}, {99, "99"}, {100, "100"},
	} {
		if got := notificationLabel(tc.count); got != tc.want {
			t.Errorf("notificationLabel(%d) = %q, want %q", tc.count, got, tc.want)
		}
	}
}

func TestNotificationIconPriority(t *testing.T) {
	cfg := config.DefaultsNotification()
	// dnd beats the unread count.
	if got := notificationIconName(cfg, 10, true); got != cfg.IconDnd {
		t.Errorf("dnd icon = %q", got)
	}
	if got := notificationIconName(cfg, 3, false); got != cfg.IconUnread {
		t.Errorf("unread icon = %q", got)
	}
	if got := notificationIconName(cfg, 0, false); got != cfg.Icon().Name {
		t.Errorf("resting icon = %q", got)
	}
}

func TestLoadFileAppliesNotification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.notifications]\nicon-unread = \"ld-bell-ring-symbolic\"\n\n[[modules.notifications.thresholds]]\nabove = 5\nicon-color = \"status-warning\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Notification.IconUnread != "ld-bell-ring-symbolic" {
		t.Errorf("icon-unread = %q", c.Notification.IconUnread)
	}
	if len(c.Notification.Thresholds) != 1 || c.Notification.Thresholds[0].IconColor == nil {
		t.Errorf("thresholds = %+v", c.Notification.Thresholds)
	}
}

func TestNotificationModuleFollowsService(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx.Notifications = notifications.NewService()

	module, err := Create("notifications", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	label := findLabel(module.Root())
	if got := label.Text(); got != "00" {
		t.Fatalf("resting label = %q, want 00", got)
	}
	tm := module.(*notificationModule)
	icon := tm.icon

	// A notification flips the count and the icon.
	ctx.Notifications.Notify("app", 0, "", "hello", "", nil, 0)
	waitForText(t, label, "01")
	if got := icon.Name(); got != cfg.Notification.IconUnread {
		t.Errorf("unread icon = %q", got)
	}

	// DND takes priority over the count and clears the popups.
	ctx.Notifications.SetDND(true)
	waitForText(t, label, "01")
	waitHeadless(t, "the dnd icon", func() bool { return icon.Name() == cfg.Notification.IconDnd })
}

func TestNotificationModuleRequiresService(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Notifications = nil
	if _, err := Create("notifications", ctx); err == nil {
		t.Fatal("nil notification service: want an error, got a module")
	}
}
