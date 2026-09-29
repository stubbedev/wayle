package config

import (
	"github.com/BurntSushi/toml"
)

// Schema icon defaults (NotificationConfig's icon trio).
const (
	defaultNotifIconDnd    = "ld-bell-off-symbolic"
	defaultNotifIconUnread = "ld-bell-dot-symbolic"
)

// NotificationConfig is the notifications module configuration: the
// bell with the unread count and the do-not-disturb state.
type NotificationConfig struct {
	Click      ClickConfig
	LabelShow  bool
	Icon       IconConfig
	IconUnread string
	IconDnd    string
	Thresholds []ThresholdEntry
}

// DefaultsNotification returns the schema defaults.
func DefaultsNotification() NotificationConfig {
	return NotificationConfig{
		LabelShow:  true,
		Icon:       DefaultsIcon(true, "ld-bell-symbolic"),
		IconUnread: defaultNotifIconUnread,
		IconDnd:    defaultNotifIconDnd,
		Thresholds: []ThresholdEntry{},
		Click:      DefaultsClick(map[string]string{"left-click": "dropdown:notification", "right-click": "wayle notify dnd"}),
	}
}

// applyNotification overlays [modules.notifications].
func applyNotification(md toml.MetaData, prim toml.Primitive) (NotificationConfig, error) {
	cfg := DefaultsNotification()
	var doc struct {
		LabelShow  *bool       `toml:"label-show"`
		IconShow   *bool       `toml:"icon-show"`
		IconName   *string     `toml:"icon-name"`
		IconColor  *ColorValue `toml:"icon-color"`
		IconUnread *string     `toml:"icon-unread"`
		IconDnd    *string     `toml:"icon-dnd"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if doc.IconShow != nil {
		cfg.Icon.Show = *doc.IconShow
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
	}
	if doc.IconColor != nil {
		cfg.Icon.Color = *doc.IconColor
	}
	if doc.IconUnread != nil {
		cfg.IconUnread = *doc.IconUnread
	}
	if doc.IconDnd != nil {
		cfg.IconDnd = *doc.IconDnd
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
