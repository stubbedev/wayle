package bar

import (
	"testing"

	"github.com/stubbedev/wayle/config"
)

func TestNotificationServiceRunsOnlyWhenEnabled(t *testing.T) {
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
