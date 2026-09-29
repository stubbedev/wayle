package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// MediaConfig is the media module config.
type MediaConfig struct {
	Click     ClickConfig
	Format    string
	LabelShow bool
	// LabelMaxLength truncates the label with an ellipsis; 0 disables.
	LabelMaxLength int
	Thresholds     []ThresholdEntry
}

// DefaultsMedia returns the schema defaults.
func DefaultsMedia() MediaConfig {
	return MediaConfig{
		Click:          DefaultsClick(map[string]string{"left-click": "dropdown:media"}),
		Format:         "{{ title }} - {{ artist }}",
		LabelShow:      true,
		LabelMaxLength: 35,
	}
}

// applyMedia overlays [modules.media].
func applyMedia(md toml.MetaData, prim toml.Primitive) (MediaConfig, error) {
	cfg := DefaultsMedia()
	var doc struct {
		Format        *string `toml:"format"`
		LabelShow     *bool   `toml:"label-show"`
		LabelMax      *int    `toml:"label-max-length"`
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
	if doc.LabelMax != nil {
		if *doc.LabelMax < 0 {
			return cfg, fmt.Errorf("media: label-max-length %d is negative", *doc.LabelMax)
		}
		cfg.LabelMaxLength = *doc.LabelMax
	}
	for _, entry := range doc.ThresholdList {
		if entry.Above == nil && entry.Below == nil {
			return cfg, errors.New("media: threshold needs above or below")
		}
		t := ThresholdEntry{Above: entry.Above, Below: entry.Below}
		if entry.IconColor != "" {
			cv, err := ParseColorValue(entry.IconColor)
			if err != nil {
				return cfg, fmt.Errorf("media: threshold icon-color: %w", err)
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
