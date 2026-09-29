package config

import (
	"errors"
	"fmt"

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
		LabelShow     *bool       `toml:"label-show"`
		IconShow      *bool       `toml:"icon-show"`
		IconName      *string     `toml:"icon-name"`
		IconColor     *ColorValue `toml:"icon-color"`
		IconUnread    *string     `toml:"icon-unread"`
		IconDnd       *string     `toml:"icon-dnd"`
		ThresholdList []struct {
			Above     *float64 `toml:"above"`
			Below     *float64 `toml:"below"`
			IconColor string   `toml:"icon-color"`
		} `toml:"thresholds"`
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
	for _, entry := range doc.ThresholdList {
		if entry.Above == nil && entry.Below == nil {
			return cfg, errors.New("notifications: threshold needs above or below")
		}
		th := ThresholdEntry{Above: entry.Above, Below: entry.Below}
		if entry.IconColor != "" {
			cv, err := ParseColorValue(entry.IconColor)
			if err != nil {
				return cfg, fmt.Errorf("notifications: threshold icon-color: %w", err)
			}
			th.IconColor, th.ColorSet = cv, true
		}
		cfg.Thresholds = append(cfg.Thresholds, th)
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
