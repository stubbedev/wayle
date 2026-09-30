package config

import (
	"errors"
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
	Enabled bool
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button     ButtonConfig
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
		Button:           DefaultsButton(buttonColors("auto", "green", "green", "bg-surface-elevated", "green"), TokenGreen, true, 0),
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
		Enabled          *bool             `toml:"enabled"`
		IconName         *string           `toml:"icon-name"`
		IconUnread       *string           `toml:"icon-unread"`
		IconDnd          *string           `toml:"icon-dnd"`
		PopupDurationMS  *int              `toml:"popup-duration"`
		PopupMaxVisible  *int              `toml:"popup-max-visible"`
		PopupPosition    *string           `toml:"popup-position"`
		PopupGap         *float64          `toml:"popup-gap"`
		PopupMonitor     *string           `toml:"popup-monitor"`
		PopupStacking    *string           `toml:"popup-stacking-order"`
		PopupCloseAction *string           `toml:"popup-close-behavior"`
		PopupHoverPause  *bool             `toml:"popup-hover-pause"`
		Thresholds       *[]ThresholdEntry `toml:"thresholds"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
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
		{doc.PopupCloseAction, &cfg.PopupCloseAction, []string{"dismiss", "remove"}, "popup-close-behavior"},
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
	setIf(doc.Thresholds, &cfg.Thresholds)
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	button.mirrorIcon(&cfg.Icon)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
