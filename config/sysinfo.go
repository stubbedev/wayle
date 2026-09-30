package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// SysinfoConfig is the shared shape of the cpu/ram/storage poll
// modules: a format, an interval, and thresholds.
type SysinfoConfig struct {
	Click ClickConfig
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button     ButtonConfig
	Format     string
	LabelShow  bool
	PollMs     int
	Thresholds []ThresholdEntry
	Icon       IconConfig
	// Path is the storage module's mount point (empty = /).
	Path string
}

// applySysinfo decodes one poll module's table on top of defaults.
func applySysinfo(md toml.MetaData, prim toml.Primitive, defaults SysinfoConfig, withPath bool) (SysinfoConfig, error) {
	var doc struct {
		Format    *string           `toml:"format"`
		PollMs    *int              `toml:"poll-interval-ms"`
		Threshold *[]ThresholdEntry `toml:"thresholds"`
		Path      *string           `toml:"path"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return defaults, err
	}
	if doc.Format != nil {
		defaults.Format = *doc.Format
	}
	if doc.PollMs != nil {
		defaults.PollMs = *doc.PollMs
	}
	if doc.Threshold != nil {
		defaults.Thresholds = *doc.Threshold
	}
	if withPath && doc.Path != nil {
		defaults.Path = *doc.Path
	}
	icon, err := applyIcon(md, prim, defaults.Icon)
	if err != nil {
		return defaults, err
	}
	defaults.Icon = icon
	button, err := applyButton(md, prim, defaults.Button, AllButtonKeys)
	if err != nil {
		return defaults, err
	}
	defaults.Button = button
	button.mirrorLabel(&defaults.LabelShow, nil)
	button.mirrorIcon(&defaults.Icon)
	if defaults.PollMs < 0 {
		return defaults, errors.New("poll interval is negative")
	}
	clicks, err := applyClicks(md, prim, defaults.Click)
	if err != nil {
		return defaults, err
	}
	defaults.Click = clicks
	return defaults, nil
}

// DefaultsSysinfoCpu seeds the cpu module defaults.
func DefaultsSysinfoCpu() SysinfoConfig {
	return SysinfoConfig{
		Format:    "{{ percent }}%",
		LabelShow: true,
		PollMs:    2000,
		Icon:      DefaultsIcon(true, "ld-cpu-symbolic"),
		Button:    DefaultsButton(buttonColors("auto", "blue", "blue", "bg-surface-elevated", "blue"), TokenBlue, true, 0),
	}
}

// DefaultsSysinfoRam seeds the ram module defaults.
func DefaultsSysinfoRam() SysinfoConfig {
	return SysinfoConfig{
		Format:    "{{ percent }}%",
		LabelShow: true,
		PollMs:    5000,
		Icon:      DefaultsIcon(true, "ld-memory-stick-symbolic"),
		Button:    DefaultsButton(buttonColors("auto", "green", "green", "bg-surface-elevated", "green"), TokenGreen, true, 0),
	}
}

// DefaultsSysinfoStorage seeds the storage module defaults.
func DefaultsSysinfoStorage() SysinfoConfig {
	return SysinfoConfig{
		Format:    "{{ percent }}%",
		LabelShow: true,
		PollMs:    30000,
		Icon:      DefaultsIcon(true, "ld-hard-drive-symbolic"),
		Button:    DefaultsButton(buttonColors("auto", "yellow", "yellow", "bg-surface-elevated", "yellow"), TokenYellow, true, 0),
		Path:      "/",
	}
}

// CPUConfig is the cpu module config.
type CPUConfig = SysinfoConfig

// applyCpu overlays [modules.cpu].
func applyCpu(md toml.MetaData, prim toml.Primitive) (CPUConfig, error) {
	return applySysinfo(md, prim, DefaultsSysinfoCpu(), false)
}

// RAMConfig is the ram module config.
type RAMConfig = SysinfoConfig

// applyRam overlays [modules.ram].
func applyRam(md toml.MetaData, prim toml.Primitive) (RAMConfig, error) {
	return applySysinfo(md, prim, DefaultsSysinfoRam(), false)
}

// StorageConfig is the storage module config.
type StorageConfig = SysinfoConfig

// applyStorage overlays [modules.storage].
func applyStorage(md toml.MetaData, prim toml.Primitive) (StorageConfig, error) {
	return applySysinfo(md, prim, DefaultsSysinfoStorage(), true)
}
