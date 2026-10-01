package bar

import (
	"log"

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
	attachNotificationStore(svc, notifications.StorePath)
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

// attachNotificationStore persists the history in the Rust store's file
// (init_store): a store that cannot open leaves the history in memory,
// as the Rust service carries on without persistence.
func attachNotificationStore(svc *notifications.Service, path func() (string, error)) {
	p, err := path()
	if err == nil {
		var st *notifications.Store
		if st, err = notifications.OpenStore(p); err == nil {
			err = svc.AttachStore(st)
		}
	}
	if err != nil {
		log.Printf("notifications: %v; the history will not persist across restarts", err)
	}
}
