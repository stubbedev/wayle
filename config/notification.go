package config

import (
	"errors"
	"fmt"
	"slices"

	"github.com/BurntSushi/toml"
)

// Schema icon defaults (NotificationConfig's icon trio).
const (
	defaultNotifIconDnd    = "ld-bell-off-symbolic"
	defaultNotifIconUnread = "ld-bell-dot-symbolic"
)

// Notification popup knobs (the schema's popup-* keys).
const (
	DefaultPopupDuration   = 5000
	DefaultPopupMaxVisible = 5
)

// NotificationConfig is the notifications module configuration: the
// bell with the unread count, the do-not-disturb state, and the popup
// window knobs.
type NotificationConfig struct {
	Enabled    bool
	LabelShow  bool
	Icon       IconConfig
	IconUnread string
	IconDnd    string
	Thresholds []ThresholdEntry
	// Popup window.
	PopupDurationMS  int
	PopupMaxVisible  int
	PopupPosition    string
	PopupGap         float64
	PopupMonitor     string
	PopupStacking    string
	PopupCloseAction string
	PopupHoverPause  bool
	Click            ClickConfig
}

// DefaultsNotification returns the schema defaults.
func DefaultsNotification() NotificationConfig {
	return NotificationConfig{
		Enabled:          true,
		LabelShow:        true,
		Icon:             DefaultsIcon(true, "ld-bell-symbolic"),
		IconUnread:       defaultNotifIconUnread,
		IconDnd:          defaultNotifIconDnd,
		Thresholds:       []ThresholdEntry{},
		PopupDurationMS:  DefaultPopupDuration,
		PopupMaxVisible:  DefaultPopupMaxVisible,
		PopupPosition:    "top-right",
		PopupGap:         1.0,
		PopupMonitor:     "primary",
		PopupStacking:    "newest-first",
		PopupCloseAction: "dismiss",
		PopupHoverPause:  true,
		Click:            DefaultsClick(map[string]string{"left-click": "dropdown:notification", "right-click": "wayle notify dnd"}),
	}
}

// applyNotification overlays [modules.notifications].
func applyNotification(md toml.MetaData, prim toml.Primitive) (NotificationConfig, error) {
	cfg := DefaultsNotification()
	var doc struct {
		LabelShow        *bool       `toml:"label-show"`
		Enabled          *bool       `toml:"enabled"`
		IconShow         *bool       `toml:"icon-show"`
		IconName         *string     `toml:"icon-name"`
		IconColor        *ColorValue `toml:"icon-color"`
		IconUnread       *string     `toml:"icon-unread"`
		IconDnd          *string     `toml:"icon-dnd"`
		PopupDurationMS  *int        `toml:"popup-duration"`
		PopupMaxVisible  *int        `toml:"popup-max-visible"`
		PopupPosition    *string     `toml:"popup-position"`
		PopupGap         *float64    `toml:"popup-gap"`
		PopupMonitor     *string     `toml:"popup-monitor"`
		PopupStacking    *string     `toml:"popup-stacking-order"`
		PopupCloseAction *string     `toml:"popup-close-behavior"`
		PopupHoverPause  *bool       `toml:"popup-hover-pause"`
		ThresholdList    []struct {
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
	if doc.Enabled != nil {
		cfg.Enabled = *doc.Enabled
	}
	if doc.PopupDurationMS != nil {
		cfg.PopupDurationMS = *doc.PopupDurationMS
	}
	if doc.PopupMaxVisible != nil {
		cfg.PopupMaxVisible = *doc.PopupMaxVisible
	}
	if doc.PopupPosition != nil {
		if !ValidOsdPosition(*doc.PopupPosition) || *doc.PopupPosition == OsdTop || *doc.PopupPosition == OsdBottom {
			return cfg, errors.New("notifications: unknown popup-position " + *doc.PopupPosition)
		}
		cfg.PopupPosition = *doc.PopupPosition
	}
	if doc.PopupGap != nil {
		cfg.PopupGap = *doc.PopupGap
	}
	if doc.PopupMonitor != nil {
		cfg.PopupMonitor = *doc.PopupMonitor
	}
	for _, set := range []struct {
		raw     *string
		dest    *string
		allowed []string
		name    string
	}{
		{doc.PopupStacking, &cfg.PopupStacking, []string{"newest-first", "oldest-first"}, "popup-stacking-order"},
		{doc.PopupCloseAction, &cfg.PopupCloseAction, []string{"dismiss", "close"}, "popup-close-behavior"},
	} {
		if set.raw == nil {
			continue
		}
		ok := slices.Contains(set.allowed, *set.raw)
		if !ok {
			return cfg, errors.New("notifications: unknown " + set.name + " " + *set.raw)
		}
		*set.dest = *set.raw
	}
	if doc.PopupHoverPause != nil {
		cfg.PopupHoverPause = *doc.PopupHoverPause
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
