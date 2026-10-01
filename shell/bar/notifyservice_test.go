package bar

import (
	"errors"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/notifications"
)

func TestNotificationServiceRunsOnlyWhenEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.Defaults().Notification
	cfg.Enabled = false
	if svc := startNotifications(cfg); svc != nil {
		t.Fatal("a disabled [modules.notification] started the service (it would own the bus name)")
	}
	applyNotificationConfig(nil, cfg) // a reload with the service off is a no-op

	cfg.Enabled = true
	cfg.Blocklist = []string{"noisy*"}
	svc := startNotifications(cfg)
	if svc == nil {
		t.Fatal("an enabled [modules.notification] has no service")
	}
	svc.Notify("noisy-app", 0, "", "spam", "", nil, 0)
	if got := svc.Count(); got != 0 {
		t.Fatalf("count = %d; the startup blocklist did not apply", got)
	}

	// A reload replaces the blocklist.
	cfg.Blocklist = nil
	applyNotificationConfig(svc, cfg)
	svc.Notify("noisy-app", 0, "", "now allowed", "", nil, 0)
	if got := svc.Count(); got != 1 {
		t.Fatalf("count = %d after the blocklist was cleared, want 1", got)
	}
}

// TestNotificationHistorySurvivesARestart pins init_store: a second
// service started over the same home reads the first one's history; a
// store that cannot open leaves the service running in memory.
func TestNotificationHistorySurvivesARestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.Defaults().Notification
	cfg.Enabled = true
	first := startNotifications(cfg)
	first.Notify("mail", 0, "", "Kept", "body", nil, 0)
	if again := startNotifications(cfg); again.Count() != 1 || again.Notifications()[0].Summary != "Kept" {
		t.Fatalf("restarted history = %v, want the stored notification", again.Notifications())
	}

	svc := notifications.NewService()
	attachNotificationStore(svc, func() (string, error) { return "", errors.New("no home") })
	svc.Notify("mail", 0, "", "Memory only", "", nil, 0)
	if svc.Count() != 1 {
		t.Error("a missing store stopped the history")
	}
}
