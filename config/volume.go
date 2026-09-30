package config

import (
	"github.com/BurntSushi/toml"
)

// Schema icon defaults (VolumeConfig's level-icons/icon-muted).
const (
	defaultVolumeMutedIcon = "ld-volume-x-symbolic"
)

// DefaultVolumeLevelIcons is the schema's level-icons list, low to
// maximum.
func DefaultVolumeLevelIcons() []string {
	return []string{"ld-volume-symbolic", "ld-volume-1-symbolic", "ld-volume-2-symbolic"}
}

// VolumeConfig is the volume module config.
type VolumeConfig struct {
	Click ClickConfig
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button     ButtonConfig
	Format     string
	LabelShow  bool
	Icon       IconConfig
	LevelIcons []string
	IconMuted  string
	Thresholds []ThresholdEntry
}

// DefaultsVolume returns the schema defaults.
func DefaultsVolume() VolumeConfig {
	return VolumeConfig{
		Click:      DefaultsClick(map[string]string{"left-click": "dropdown:audio", "middle-click": "wayle audio output-mute"}),
		Button:     DefaultsButton(buttonColors("auto", "red", "red", "bg-surface-elevated", "red"), TokenRed, true, 0),
		Format:     "{{ percent }}%",
		LabelShow:  true,
		Icon:       DefaultsIcon(true, "ld-volume-2-symbolic"),
		LevelIcons: DefaultVolumeLevelIcons(),
		IconMuted:  defaultVolumeMutedIcon,
	}
}

// applyVolume overlays [modules.volume].
func applyVolume(md toml.MetaData, prim toml.Primitive) (VolumeConfig, error) {
	cfg := DefaultsVolume()
	var doc struct {
		Format     *string           `toml:"format"`
		IconName   *string           `toml:"icon-name"`
		LevelIcons *[]string         `toml:"level-icons"`
		IconMuted  *string           `toml:"icon-muted"`
		Thresholds *[]ThresholdEntry `toml:"thresholds"`
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
	if doc.LevelIcons != nil {
		cfg.LevelIcons = *doc.LevelIcons
	}
	if doc.IconMuted != nil {
		cfg.IconMuted = *doc.IconMuted
	}
	setIf(&cfg.Thresholds, doc.Thresholds)
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
