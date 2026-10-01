package wallpaper

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/wallpaper"
)

func TestReloadAppliesTheGlobalFitModeOnlyWhenItChanged(t *testing.T) {
	svc := newSvc("DP-1", "DP-2")
	old := config.DefaultsWallpaper()
	svc.SetFitMode(wallpaper.FitCenter, "DP-2")

	// Another field changed: DP-2's own fit mode stays.
	next := old
	next.CyclingSameImage = true
	applyWallpaperChange(svc, old, next)
	if got, _ := svc.FitModeOf("DP-2"); got != wallpaper.FitCenter {
		t.Errorf("an unrelated change reset DP-2's fit mode to %v", got)
	}

	next.FitMode = wallpaper.FitStretch
	applyWallpaperChange(svc, old, next)
	for _, m := range []string{"DP-1", "DP-2"} {
		if got, _ := svc.FitModeOf(m); got != wallpaper.FitStretch {
			t.Errorf("%s fit mode = %v, want the new global stretch", m, got)
		}
	}
}

func TestReloadSingleImageYieldsToACyclingDirectory(t *testing.T) {
	dir := files(t, "a.png", "b.png", "single.png")
	svc := newSvc("DP-1")
	old := config.DefaultsWallpaper()

	next := old
	next.Wallpaper = filepath.Join(dir, "single.png")
	applyWallpaperChange(svc, old, next)
	if got, _ := svc.Wallpaper("DP-1"); got != next.Wallpaper {
		t.Fatalf("single image = %q, want %q", got, next.Wallpaper)
	}

	// With a cycling directory set, a new single image is ignored.
	cycling := next
	cycling.CyclingDirectory = dir
	applyWallpaperChange(svc, next, cycling)
	if svc.CyclingConfig() == nil {
		t.Fatal("setting cycling-directory did not start cycling")
	}
	changed := cycling
	changed.Wallpaper = filepath.Join(dir, "a.png")
	before, _ := svc.Wallpaper("DP-1")
	applyWallpaperChange(svc, cycling, changed)
	if got, _ := svc.Wallpaper("DP-1"); got != before {
		t.Errorf("a single image replaced the cycle's %q with %q", before, got)
	}
}

func TestReloadClearingTheCycleRestoresTheConfiguredImages(t *testing.T) {
	dir := files(t, "a.png", "b.png", "single.png", "own.png")
	svc := newSvc("DP-1", "DP-2")
	cycling := config.DefaultsWallpaper()
	cycling.CyclingDirectory = dir
	cycling.Wallpaper = filepath.Join(dir, "single.png")
	cycling.Monitors = []config.MonitorWallpaperConfig{{Name: "DP-2", FitMode: wallpaper.FitFill, Wallpaper: filepath.Join(dir, "own.png")}}
	applyWallpaperChange(svc, config.DefaultsWallpaper(), cycling)
	if svc.CyclingConfig() == nil {
		t.Fatal("cycling did not start")
	}

	cleared := cycling
	cleared.CyclingDirectory = ""
	applyWallpaperChange(svc, cycling, cleared)
	if svc.CyclingConfig() != nil {
		t.Fatal("clearing cycling-directory left cycling on")
	}
	// The single image is applied last, to every monitor (the Rust
	// watcher's order).
	for _, m := range []string{"DP-1", "DP-2"} {
		if got, _ := svc.Wallpaper(m); got != cycling.Wallpaper {
			t.Errorf("%s = %q, want the restored single image", m, got)
		}
	}
}

func TestReloadAppliesChangedMonitorEntries(t *testing.T) {
	dir := files(t, "own.png")
	svc := newSvc("DP-1", "DP-2")
	old := config.DefaultsWallpaper()
	next := old
	next.Monitors = []config.MonitorWallpaperConfig{
		{Name: "DP-1", FitMode: wallpaper.FitCenter, Wallpaper: filepath.Join(dir, "own.png")},
		{Name: "", FitMode: wallpaper.FitStretch, Wallpaper: filepath.Join(dir, "own.png")},
	}
	applyWallpaperChange(svc, old, next)
	if got, _ := svc.Wallpaper("DP-1"); got != filepath.Join(dir, "own.png") {
		t.Errorf("DP-1 = %q, want its entry's image", got)
	}
	if got, _ := svc.FitModeOf("DP-1"); got != wallpaper.FitCenter {
		t.Errorf("DP-1 fit = %v, want center", got)
	}
	if got, _ := svc.Wallpaper("DP-2"); got != "" {
		t.Errorf("DP-2 = %q; a nameless entry must not apply anywhere", got)
	}
}

// SetConfig hands the service the new theming monitor and keeps the new
// [wallpaper] for monitors that appear later.
func TestSetConfigUpdatesThemingAndTheHotplugSnapshot(t *testing.T) {
	svc := newSvc("DP-1")
	s := &Shell{svc: svc, cfg: config.DefaultsWallpaper()}
	cfg := config.Defaults()
	cfg.Styling.ColorExtractor.ThemingMonitor = "DP-1"
	cfg.Wallpaper.FitMode = wallpaper.FitFit
	s.SetConfig(cfg)
	if got := svc.ThemingMonitor(); got != "DP-1" {
		t.Errorf("theming monitor = %q, want DP-1", got)
	}
	if got := hotplugFitMode(s.cfg, "HDMI-1"); got != wallpaper.FitFit {
		t.Errorf("hotplug fit mode = %v, want the reloaded tile", got)
	}
}
