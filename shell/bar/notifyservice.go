package bar

import (
	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/notifications"
)

// startNotifications builds the notification service the way the Rust
// bootstrap does: only when [modules.notification] is enabled, so
// another daemon can own the bus name otherwise (and the module, its
// dropdown, and the popups are absent), seeded with the blocklist.
func startNotifications(cfg config.NotificationConfig) *notifications.Service {
	if !cfg.Enabled {
		return nil
	}
	svc := notifications.NewService()
	applyNotificationConfig(svc, cfg)
	return svc
}

// applyNotificationConfig hands a running service the reloadable keys
// (notification.rs's blocklist watcher); a disabled service stays off
// until the next start, as in the Rust shell.
func applyNotificationConfig(svc *notifications.Service, cfg config.NotificationConfig) {
	if svc == nil {
		return
	}
	svc.SetBlocklist(cfg.Blocklist)
}
