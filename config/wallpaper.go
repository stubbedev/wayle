package config

import (
	"fmt"
	"log"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/stubbedev/wayle/service/wallpaper"
)

// cyclingIntervalMin is CyclingInterval's floor, in minutes.
const cyclingIntervalMin = 1

// WallpaperConfig is the [wallpaper] section
// (crates/wayle-config/src/schemas/wallpaper): what each monitor shows
// and how. Resolution order: a cycling directory, else the single
// Wallpaper image; Monitors entries apply on top.
type WallpaperConfig struct {
	// Wallpaper is one image for every monitor; empty uses cycling
	// and/or the per-monitor entries.
	Wallpaper string
	// FitMode is the global scaling, overridden per monitor.
	FitMode wallpaper.FitMode
	// CyclingDirectory enables cycling when set; it takes precedence
	// over Wallpaper.
	CyclingDirectory string
	CyclingMode      wallpaper.CyclingMode
	// CyclingIntervalMins is at least 1.
	CyclingIntervalMins uint64
	// CyclingSameImage shows one shuffle image on every monitor.
	CyclingSameImage bool
	Monitors         []MonitorWallpaperConfig
}

// MonitorWallpaperConfig is one [[wallpaper.monitors]] entry, keyed by
// connector name.
type MonitorWallpaperConfig struct {
	Name      string
	FitMode   wallpaper.FitMode
	Wallpaper string
}

// DefaultsWallpaper returns the schema defaults.
func DefaultsWallpaper() WallpaperConfig {
	return WallpaperConfig{
		FitMode:             wallpaper.FitFill,
		CyclingMode:         wallpaper.Sequential,
		CyclingIntervalMins: 15,
	}
}

// Monitor finds the entry for a connector.
func (w WallpaperConfig) Monitor(name string) (MonitorWallpaperConfig, bool) {
	i := slices.IndexFunc(w.Monitors, func(m MonitorWallpaperConfig) bool { return m.Name == name })
	if i < 0 {
		return MonitorWallpaperConfig{}, false
	}
	return w.Monitors[i], true
}

// exactEnum parses a serde lowercase enum value: exact spelling only,
// though the parser itself may be case-insensitive for D-Bus.
func exactEnum[T fmt.Stringer](key, value string, parse func(string) (T, error)) (T, error) {
	v, err := parse(value)
	if err == nil && v.String() != value {
		err = fmt.Errorf("unknown variant %q", value)
	}
	if err != nil {
		var zero T
		return zero, fmt.Errorf("wallpaper: %s: %w", key, err)
	}
	return v, nil
}

// applyWallpaper overlays [wallpaper].
func applyWallpaper(md toml.MetaData, prim toml.Primitive) (WallpaperConfig, error) {
	cfg := DefaultsWallpaper()
	var doc struct {
		Wallpaper        *string `toml:"wallpaper"`
		FitMode          *string `toml:"fit-mode"`
		CyclingDirectory *string `toml:"cycling-directory"`
		CyclingMode      *string `toml:"cycling-mode"`
		CyclingInterval  *int64  `toml:"cycling-interval-mins"`
		CyclingSameImage *bool   `toml:"cycling-same-image"`
		Monitors         []struct {
			Name      *string `toml:"name"`
			FitMode   *string `toml:"fit-mode"`
			Wallpaper *string `toml:"wallpaper"`
		} `toml:"monitors"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Wallpaper != nil {
		cfg.Wallpaper = *doc.Wallpaper
	}
	if doc.FitMode != nil {
		fit, err := exactEnum("fit-mode", *doc.FitMode, wallpaper.ParseFitMode)
		if err != nil {
			return cfg, err
		}
		cfg.FitMode = fit
	}
	if doc.CyclingDirectory != nil {
		cfg.CyclingDirectory = *doc.CyclingDirectory
	}
	if doc.CyclingMode != nil {
		mode, err := exactEnum("cycling-mode", *doc.CyclingMode, wallpaper.ParseCyclingMode)
		if err != nil {
			return cfg, err
		}
		cfg.CyclingMode = mode
	}
	if doc.CyclingInterval != nil {
		mins := *doc.CyclingInterval
		if mins < 0 {
			return cfg, fmt.Errorf("wallpaper: cycling-interval-mins: invalid value %d, expected u64", mins)
		}
		if mins < cyclingIntervalMin {
			// CyclingInterval clamps with a warning rather than failing.
			log.Printf("config: cycling interval %d below minimum (%d), clamped", mins, cyclingIntervalMin)
			mins = cyclingIntervalMin
		}
		cfg.CyclingIntervalMins = uint64(mins)
	}
	if doc.CyclingSameImage != nil {
		cfg.CyclingSameImage = *doc.CyclingSameImage
	}
	for i, m := range doc.Monitors {
		if m.Name == nil {
			return cfg, fmt.Errorf("wallpaper: monitors[%d]: missing field `name`", i)
		}
		entry := MonitorWallpaperConfig{Name: *m.Name, FitMode: wallpaper.FitFill}
		if m.FitMode != nil {
			fit, err := exactEnum("monitors.fit-mode", *m.FitMode, wallpaper.ParseFitMode)
			if err != nil {
				return cfg, err
			}
			entry.FitMode = fit
		}
		if m.Wallpaper != nil {
			entry.Wallpaper = *m.Wallpaper
		}
		cfg.Monitors = append(cfg.Monitors, entry)
	}
	return cfg, nil
}
