package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// ThresholdEntry maps a numeric range to a color override; at least
// one bound must be set, and both set means AND.
type ThresholdEntry struct {
	Above     *float64
	Below     *float64
	IconColor ColorValue
	ColorSet  bool
}

// Matches reports whether value falls in the entry's range.
func (t ThresholdEntry) Matches(value float64) bool {
	if t.Above != nil && value <= *t.Above {
		return false
	}
	if t.Below != nil && value >= *t.Below {
		return false
	}
	return t.Above != nil || t.Below != nil
}

// BatteryConfig is the battery module config.
type BatteryConfig struct {
	Click        ClickConfig
	Format       string
	LabelShow    bool
	Icon         IconConfig
	LevelIcons   []string
	AlertIcon    string
	ChargingIcon string
	Thresholds   []ThresholdEntry
}

// Schema icon defaults (BatteryConfig's level-icons etc.).
const (
	defaultAlertIcon    = "md-battery_android_alert-symbolic"
	defaultChargingIcon = "md-battery_android_frame_bolt-symbolic"
)

// DefaultBatteryLevelIcons is the schema's level-icons list, empty to
// full.
func DefaultBatteryLevelIcons() []string {
	return []string{
		"md-battery_android_0-symbolic",
		"md-battery_android_frame_1-symbolic",
		"md-battery_android_frame_2-symbolic",
		"md-battery_android_frame_3-symbolic",
		"md-battery_android_frame_4-symbolic",
		"md-battery_android_frame_5-symbolic",
		"md-battery_android_frame_6-symbolic",
		"md-battery_android_frame_full-symbolic",
	}
}

// DefaultsBattery returns the schema defaults.
func DefaultsBattery() BatteryConfig {
	return BatteryConfig{
		Click:        DefaultsClick(map[string]string{"left-click": "dropdown:battery"}),
		Format:       "{{ percent }}%",
		LabelShow:    true,
		Icon:         DefaultsIcon(true, defaultAlertIcon),
		LevelIcons:   DefaultBatteryLevelIcons(),
		AlertIcon:    defaultAlertIcon,
		ChargingIcon: defaultChargingIcon,
	}
}

// applyBattery overlays [modules.battery].
func applyBattery(md toml.MetaData, prim toml.Primitive) (BatteryConfig, error) {
	cfg := DefaultsBattery()
	var doc struct {
		Format        *string     `toml:"format"`
		LabelShow     *bool       `toml:"label-show"`
		IconShow      *bool       `toml:"icon-show"`
		IconName      *string     `toml:"icon-name"`
		IconColor     *ColorValue `toml:"icon-color"`
		LevelIcons    *[]string   `toml:"level-icons"`
		AlertIcon     *string     `toml:"alert-icon"`
		ChargingIcon  *string     `toml:"charging-icon"`
		ThresholdList []struct {
			Above     *float64 `toml:"above"`
			Below     *float64 `toml:"below"`
			IconColor string   `toml:"icon-color"`
		} `toml:"thresholds"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
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
	if doc.LevelIcons != nil {
		cfg.LevelIcons = *doc.LevelIcons
	}
	if doc.AlertIcon != nil {
		cfg.AlertIcon = *doc.AlertIcon
	}
	if doc.ChargingIcon != nil {
		cfg.ChargingIcon = *doc.ChargingIcon
	}
	for _, entry := range doc.ThresholdList {
		if entry.Above == nil && entry.Below == nil {
			return cfg, errors.New("battery: threshold needs above or below")
		}
		t := ThresholdEntry{Above: entry.Above, Below: entry.Below}
		if entry.IconColor != "" {
			cv, err := ParseColorValue(entry.IconColor)
			if err != nil {
				return cfg, fmt.Errorf("battery: threshold icon-color: %w", err)
			}
			t.IconColor, t.ColorSet = cv, true
		}
		cfg.Thresholds = append(cfg.Thresholds, t)
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
