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
	Click      ClickConfig
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
		Format        *string          `toml:"format"`
		LabelShow     *bool            `toml:"label-show"`
		IconShow      *bool            `toml:"icon-show"`
		IconName      *string          `toml:"icon-name"`
		IconColor     *ColorValue      `toml:"icon-color"`
		LevelIcons    *[]string        `toml:"level-icons"`
		IconMuted     *string          `toml:"icon-muted"`
		ThresholdList []ThresholdEntry `toml:"thresholds"`
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
	if doc.IconMuted != nil {
		cfg.IconMuted = *doc.IconMuted
	}
	if doc.ThresholdList != nil {
		cfg.Thresholds = doc.ThresholdList
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
