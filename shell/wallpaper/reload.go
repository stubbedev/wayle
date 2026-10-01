package wallpaper

import (
	"log"
	"slices"
	"time"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/wallpaper"
	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

// SetConfig applies a reloaded config: the extraction settings
// (color_extractor.rs) and the [wallpaper] changes (watchers/
// wallpaper.rs). Monitors that appear later take their state from the
// new snapshot. Call it on the loop goroutine.
func (s *Shell) SetConfig(cfg *config.Config) {
	old := s.cfg
	s.cfg = cfg.Wallpaper
	applyStyling(s.svc, cfg.Styling.ColorExtractor)
	applyWallpaperChange(s.svc, old, cfg.Wallpaper)
}

// applyStyling hands the service the extraction tool and theming
// monitor; the service re-extracts only when either changed.
func applyStyling(svc *wallpaper.Service, cfg config.ColorExtractorConfig) {
	svc.SetExtractor(extract.FromConfig(cfg))
	svc.SetThemingMonitor(cfg.ThemingMonitor)
}

// applyWallpaperChange applies what changed between two [wallpaper]
// snapshots, each field the way its Rust watcher does: the global fit
// mode to every monitor; a new single image unless a cycling directory
// takes precedence; cycling started (or, with the directory cleared,
// stopped and the per-monitor and single images restored) when the
// directory or mode changed; the interval and shared-image flags; and
// every [[wallpaper.monitors]] entry when the list changed.
func applyWallpaperChange(svc *wallpaper.Service, old, next config.WallpaperConfig) {
	if next.FitMode != old.FitMode {
		svc.SetFitMode(next.FitMode, "")
	}
	if next.Wallpaper != old.Wallpaper && next.Wallpaper != "" && next.CyclingDirectory == "" {
		if err := svc.SetWallpaper(next.Wallpaper, ""); err != nil {
			log.Printf("wallpaper: cannot apply single-file wallpaper from config change: %v", err)
		}
	}
	if next.CyclingDirectory != old.CyclingDirectory || next.CyclingMode != old.CyclingMode {
		if next.CyclingDirectory == "" {
			svc.StopCycling()
			restoreMonitorWallpapers(svc, next.Monitors)
			if next.Wallpaper != "" {
				if err := svc.SetWallpaper(next.Wallpaper, ""); err != nil {
					log.Printf("wallpaper: cannot restore single-file wallpaper: %v", err)
				}
			}
		} else if err := svc.StartCycling(next.CyclingDirectory, cyclingInterval(next), next.CyclingMode); err != nil {
			log.Printf("wallpaper: could not apply cycling config change: %v", err)
		}
	}
	if next.CyclingIntervalMins != old.CyclingIntervalMins {
		svc.SetCyclingInterval(cyclingInterval(next))
	}
	if next.CyclingSameImage != old.CyclingSameImage {
		svc.SetSharedCycle(next.CyclingSameImage)
	}
	if !slices.Equal(next.Monitors, old.Monitors) {
		for _, m := range next.Monitors {
			applyMonitorEntry(svc, m)
		}
	}
}

func cyclingInterval(cfg config.WallpaperConfig) time.Duration {
	return time.Duration(cfg.CyclingIntervalMins) * time.Minute
}

// applyMonitorEntry is apply_monitor_config_change: the entry's fit
// mode, then its image when it names one.
func applyMonitorEntry(svc *wallpaper.Service, m config.MonitorWallpaperConfig) {
	if m.Name == "" {
		return
	}
	svc.SetFitMode(m.FitMode, m.Name)
	if m.Wallpaper == "" {
		return
	}
	if err := svc.SetWallpaper(m.Wallpaper, m.Name); err != nil {
		log.Printf("wallpaper: could not apply wallpaper for %s from config change: %v", m.Name, err)
	}
}

// restoreMonitorWallpapers puts the per-monitor images back once
// cycling is cleared.
func restoreMonitorWallpapers(svc *wallpaper.Service, monitors []config.MonitorWallpaperConfig) {
	for _, m := range monitors {
		if m.Name == "" || m.Wallpaper == "" {
			continue
		}
		if err := svc.SetWallpaper(m.Wallpaper, m.Name); err != nil {
			log.Printf("wallpaper: cannot restore wallpaper for %s: %v", m.Name, err)
		}
	}
}
