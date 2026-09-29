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
	Click     ClickConfig
	Format    string
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
		IconMuted: defaultMicMutedIcon,
	}
}

// applyMicrophone overlays [modules.microphone].
func applyMicrophone(md toml.MetaData, prim toml.Primitive) (MicrophoneConfig, error) {
	cfg := DefaultsMicrophone()
	var doc struct {
		Format    *string     `toml:"format"`
		LabelShow *bool       `toml:"label-show"`
		IconShow  *bool       `toml:"icon-show"`
		IconName  *string     `toml:"icon-name"`
		IconColor *ColorValue `toml:"icon-color"`
		IconMuted *string     `toml:"icon-muted"`
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
	if doc.IconMuted != nil {
		cfg.IconMuted = *doc.IconMuted
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
