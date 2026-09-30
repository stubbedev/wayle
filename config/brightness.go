package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// BrightnessConfig is the brightness module config.
type BrightnessConfig struct {
	Click ClickConfig
	// Button is the bar-button key set; LabelShow mirrors its
	// label-show.
	Button     ButtonConfig
	Format     string
	LabelShow  bool
	MinBright  int
	EnableExt  bool
	Thresholds []ThresholdEntry
}

// DefaultsBrightness returns the schema defaults.
func DefaultsBrightness() BrightnessConfig {
	return BrightnessConfig{
		Click:     DefaultsClick(map[string]string{"left-click": "dropdown:brightness", "scroll-up": "brightness:5", "scroll-down": "brightness:-5"}),
		Button:    DefaultsButton(buttonColors("auto", "yellow", "yellow", "bg-surface-elevated", "yellow"), TokenYellow, true, 0),
		Format:    "{{ percent }}%",
		LabelShow: true,
		MinBright: 1,
		EnableExt: true,
	}
}

// applyBrightness overlays [modules.brightness].
func applyBrightness(md toml.MetaData, prim toml.Primitive) (BrightnessConfig, error) {
	cfg := DefaultsBrightness()
	var doc struct {
		Format     *string           `toml:"format"`
		MinBright  *int              `toml:"min-brightness"`
		EnableExt  *bool             `toml:"enable-external"`
		Thresholds *[]ThresholdEntry `toml:"thresholds"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.MinBright != nil {
		if *doc.MinBright < 0 || *doc.MinBright > 100 {
			return cfg, fmt.Errorf("brightness: min-brightness %d outside 0-100", *doc.MinBright)
		}
		cfg.MinBright = *doc.MinBright
	}
	if doc.EnableExt != nil {
		cfg.EnableExt = *doc.EnableExt
	}
	setIf(doc.Thresholds, &cfg.Thresholds)
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
