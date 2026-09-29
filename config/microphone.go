package config

import (
	"github.com/BurntSushi/toml"
)

// MicrophoneConfig is the microphone module config.
type MicrophoneConfig struct {
	Click ClickConfig
	// Format renders "{{ percent }}" like the level modules; an empty
	// format means the icon-only default (the percent becomes the label
	// anyway on this label-only surface).
	Format    string
	LabelShow bool
}

// DefaultsMicrophone returns the schema defaults.
func DefaultsMicrophone() MicrophoneConfig {
	return MicrophoneConfig{
		Click:     DefaultsClick(map[string]string{"left-click": "dropdown:audio", "middle-click": "wayle audio input-mute"}),
		Format:    "{{ percent }}%",
		LabelShow: true,
	}
}

// applyMicrophone overlays [modules.microphone].
func applyMicrophone(md toml.MetaData, prim toml.Primitive) (MicrophoneConfig, error) {
	cfg := DefaultsMicrophone()
	var doc struct {
		Format    *string `toml:"format"`
		LabelShow *bool   `toml:"label-show"`
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
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
