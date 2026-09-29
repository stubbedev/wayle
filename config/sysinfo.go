package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// SysinfoConfig is the shared shape of the cpu/ram/storage poll
// modules: a format, an interval, and thresholds.
type SysinfoConfig struct {
	Click      ClickConfig
	Format     string
	LabelShow  bool
	PollMs     int
	Thresholds []ThresholdEntry
	// Path is the storage module's mount point (empty = /).
	Path string
}

// applySysinfo decodes one poll module's table on top of defaults.
func applySysinfo(md toml.MetaData, prim toml.Primitive, defaults SysinfoConfig, withPath bool) (SysinfoConfig, error) {
	var doc struct {
		Format    *string           `toml:"format"`
		LabelShow *bool             `toml:"label-show"`
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
	if doc.LabelShow != nil {
		defaults.LabelShow = *doc.LabelShow
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
	}
}

// DefaultsSysinfoRam seeds the ram module defaults.
func DefaultsSysinfoRam() SysinfoConfig {
	return SysinfoConfig{
		Format:    "{{ percent }}%",
		LabelShow: true,
		PollMs:    5000,
	}
}

// DefaultsSysinfoStorage seeds the storage module defaults.
func DefaultsSysinfoStorage() SysinfoConfig {
	return SysinfoConfig{
		Format:    "{{ percent }}%",
		LabelShow: true,
		PollMs:    30000,
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
