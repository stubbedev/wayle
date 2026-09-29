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
	Click      ClickConfig
	Format     string
	LabelShow  bool
	Thresholds []ThresholdEntry
}

// DefaultsBattery returns the schema defaults.
func DefaultsBattery() BatteryConfig {
	return BatteryConfig{
		Click:     DefaultsClick(map[string]string{"left-click": "dropdown:battery"}),
		Format:    "{{ percent }}%",
		LabelShow: true,
	}
}

// applyBattery overlays [modules.battery].
func applyBattery(md toml.MetaData, prim toml.Primitive) (BatteryConfig, error) {
	cfg := DefaultsBattery()
	var doc struct {
		Format        *string `toml:"format"`
		LabelShow     *bool   `toml:"label-show"`
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
