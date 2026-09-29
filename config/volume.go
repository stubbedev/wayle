package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// VolumeConfig is the volume module config.
type VolumeConfig struct {
	Click      ClickConfig
	Format     string
	LabelShow  bool
	MuteShows  bool
	Thresholds []ThresholdEntry
}

// DefaultsVolume returns the schema defaults.
func DefaultsVolume() VolumeConfig {
	return VolumeConfig{
		Click:     DefaultsClick(map[string]string{"left-click": "dropdown:audio", "middle-click": "wayle audio output-mute"}),
		Format:    "{{ percent }}%",
		LabelShow: true,
		MuteShows: true,
	}
}

// applyVolume overlays [modules.volume].
func applyVolume(md toml.MetaData, prim toml.Primitive) (VolumeConfig, error) {
	cfg := DefaultsVolume()
	var doc struct {
		Format        *string `toml:"format"`
		LabelShow     *bool   `toml:"label-show"`
		MuteShows     *bool   `toml:"icon-muted"`
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
	if doc.MuteShows != nil {
		cfg.MuteShows = *doc.MuteShows
	}
	for _, entry := range doc.ThresholdList {
		if entry.Above == nil && entry.Below == nil {
			return cfg, errors.New("volume: threshold needs above or below")
		}
		t := ThresholdEntry{Above: entry.Above, Below: entry.Below}
		if entry.IconColor != "" {
			cv, err := ParseColorValue(entry.IconColor)
			if err != nil {
				return cfg, fmt.Errorf("volume: threshold icon-color: %w", err)
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
