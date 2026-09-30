package config

import (
	"github.com/BurntSushi/toml"
)

// Schema icon defaults (MicrophoneConfig's icon-active/icon-muted).
const (
	defaultMicActiveIcon = "ld-mic-symbolic"
	defaultMicMutedIcon  = "ld-mic-off-symbolic"
)

// MicrophoneConfig is the microphone module config.
type MicrophoneConfig struct {
	Click  ClickConfig
	Format string
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button    ButtonConfig
	LabelShow bool
	Icon      IconConfig
	IconMuted string
}

// DefaultsMicrophone returns the schema defaults.
func DefaultsMicrophone() MicrophoneConfig {
	return MicrophoneConfig{
		Click:     DefaultsClick(map[string]string{"left-click": "dropdown:audio", "middle-click": "wayle audio input-mute"}),
		Format:    "{{ percent }}%",
		LabelShow: true,
		Icon:      DefaultsIcon(true, defaultMicActiveIcon),
		Button:    DefaultsButton(buttonColors("auto", "red", "red", "bg-surface-elevated", "red"), TokenRed, true, 0),
		IconMuted: defaultMicMutedIcon,
	}
}

// applyMicrophone overlays [modules.microphone].
func applyMicrophone(md toml.MetaData, prim toml.Primitive) (MicrophoneConfig, error) {
	cfg := DefaultsMicrophone()
	var doc struct {
		Format    *string `toml:"format"`
		IconName  *string `toml:"icon-name"`
		IconMuted *string `toml:"icon-muted"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
	}
	if doc.IconMuted != nil {
		cfg.IconMuted = *doc.IconMuted
	}
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
