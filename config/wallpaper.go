package config

import (
	"log"
	"math"
	"reflect"
	"slices"
)

// WallpaperConfig is the [wallpaper] section
// (crates/wayle-config/src/schemas/wallpaper): what each monitor shows
// and how. Resolution order: a cycling directory, else the single
// Wallpaper image; Monitors entries apply on top.
//
// Wallpaper rendering, cycling, and per-monitor overrides.
type WallpaperConfig struct {
	// A single image file to use as the wallpaper on all monitors. Leave empty
	// to use cycling and/or per-monitor overrides instead.
	Wallpaper string `cfg:"wallpaper"`
	// How the wallpaper is scaled to the screen. Per-monitor entries in
	// `[[wallpaper.monitors]]` override this.
	FitMode FitMode `cfg:"fit-mode"`
	// Directory of images to cycle through. Set it to enable cycling; leave
	// empty to disable. Takes precedence over the single `wallpaper` image.
	CyclingDirectory string `cfg:"cycling-directory"`
	// Wallpaper cycling order.
	CyclingMode CyclingMode `cfg:"cycling-mode"`
	// Time between wallpaper changes in minutes.
	CyclingIntervalMins CyclingInterval `cfg:"cycling-interval-mins"`
	// Show the same cycling wallpaper on all monitors. Only affects shuffle
	// mode since sequential already displays the same image.
	CyclingSameImage bool `cfg:"cycling-same-image"`
	// Per-monitor wallpaper and fit mode settings. Each entry targets a
	// monitor by connector name. See [`MonitorWallpaperConfig`] for the
	// available fields.
	//
	// ## Example
	//
	// ```toml
	// [[wallpaper.monitors]]
	// name = "DP-1"
	// wallpaper = "/home/me/pictures/wall-primary.png"
	// fit-mode = "fill"
	//
	// [[wallpaper.monitors]]
	// name = "HDMI-1"
	// wallpaper = "/home/me/pictures/wall-secondary.png"
	// fit-mode = "fit"
	// ```
	Monitors []MonitorWallpaperConfig `cfg:"monitors"`
}

// DefaultsWallpaper returns the schema defaults.
func DefaultsWallpaper() WallpaperConfig {
	return WallpaperConfig{
		FitMode:             FitFill,
		CyclingMode:         CyclingSequential,
		CyclingIntervalMins: CyclingIntervalDefault,
		Monitors:            []MonitorWallpaperConfig{},
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

// MonitorWallpaperConfig is one [[wallpaper.monitors]] entry, keyed by
// connector name.
//
// Per-monitor wallpaper configuration.
type MonitorWallpaperConfig struct {
	// Monitor name (e.g., "HDMI-1", "DP-1").
	Name string `cfg:"name,required"`
	// Image scaling mode for this monitor.
	FitMode FitMode `cfg:"fit-mode,default"`
	// Wallpaper image path for this monitor.
	Wallpaper string `cfg:"wallpaper,default"`
}

func (MonitorWallpaperConfig) noStructDefault() {}

// FitMode is how an image is scaled to its monitor (types/fit_mode.rs).
//
// Image scaling mode.
type FitMode string

// Fit modes; Fill is the default.
const (
	// Scale to cover entire display, cropping excess.
	FitFill FitMode = "fill"
	// Scale to fit within display, letterboxing if needed.
	FitFit FitMode = "fit"
	// Display at original size, centered.
	FitCenter FitMode = "center"
	// Stretch to exactly fill, ignoring aspect ratio.
	FitStretch FitMode = "stretch"
)

var _ = registerEnum(FitFill, FitFit, FitCenter, FitStretch)

// FitModes is every fit mode, in schema order.
var FitModes = []FitMode{FitFill, FitFit, FitCenter, FitStretch}

// CyclingMode is the order images cycle in (types/cycling.rs).
//
// Wallpaper cycling order.
type CyclingMode string

// Cycling modes; Sequential is the default.
const (
	// Alphabetical order.
	CyclingSequential CyclingMode = "sequential"
	// Random order.
	CyclingShuffle CyclingMode = "shuffle"
)

var _ = registerEnum(CyclingSequential, CyclingShuffle)

// CyclingModes is every cycling mode, in schema order.
var CyclingModes = []CyclingMode{CyclingSequential, CyclingShuffle}

// CyclingInterval is the cycling period in minutes, at least 1
// (types/cycling.rs CyclingInterval).
//
// Cycling interval in minutes, minimum 1.
type CyclingInterval uint64

// The interval's floor and default.
const (
	CyclingIntervalMin     CyclingInterval = 1
	CyclingIntervalDefault CyclingInterval = 15
)

// UnmarshalConfig implements Unmarshaler: below the floor clamps with
// the Rust warning rather than failing.
func (c *CyclingInterval) UnmarshalConfig(v any) error {
	n, err := decodeClamped[uint64](v, 0, math.MaxUint64, "cycling interval")
	if err != nil {
		return err
	}
	if CyclingInterval(n) < CyclingIntervalMin {
		log.Printf("config: cycling interval %d below minimum (%d), clamped", n, CyclingIntervalMin)
		n = uint64(CyclingIntervalMin)
	}
	*c = CyclingInterval(n)
	return nil
}

// MarshalConfig implements Marshaler.
func (c CyclingInterval) MarshalConfig() any { return int64(c) }

func (CyclingInterval) configSchema(*schemaGen) Schema {
	return rangedSchema(reflect.TypeFor[CyclingInterval](), int64(CyclingIntervalMin), nil)
}

// setDefaults is the entry's serde field defaults: fit-mode is Fill.
func (m *MonitorWallpaperConfig) setDefaults() { m.FitMode = FitFill }

// String is the config spelling.
func (m FitMode) String() string { return string(m) }

// String is the config spelling.
func (m CyclingMode) String() string { return string(m) }
