// Package wallpaper ports crates/wayle-wallpaper: per-monitor wallpaper
// state, fit modes, directory cycling (sequential or shuffled, shared
// or per monitor, rescanned on directory changes), color extraction
// for theming, and the com.wayle.Wallpaper1 D-Bus interface the
// `wayle wallpaper` CLI drives.
//
// The service only tracks state; the shell renders it (shell/wallpaper)
// by subscribing to Changes and reading Monitors. The shell also feeds
// it the outputs it sees (RegisterMonitor/UnregisterMonitor), where the
// Rust service ran a second Wayland connection of its own for that.
package wallpaper

import (
	"context"
	"log"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/wayle/internal/feed"
	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

// Options configure a Service (WallpaperServiceBuilder).
type Options struct {
	// Extractor is the color extraction tool and its parameters.
	Extractor extract.Config
	// ThemingMonitor drives color extraction; empty falls back to the
	// lowest-named monitor.
	ThemingMonitor string
	// SharedCycle shows the same image on every monitor in shuffle mode.
	SharedCycle bool
	// Rand seeds shuffles and random starting indices; nil picks a
	// random seed. Tests pass a fixed one.
	Rand *rand.Rand
}

// Service is the wallpaper state manager (WallpaperService). All
// methods are safe for concurrent use; Run drives the cycling timer,
// the directory watcher, and color extraction.
type Service struct {
	mu             sync.Mutex
	monitors       map[string]MonitorState
	cycling        *Cycling
	sharedCycle    bool
	themingMonitor string
	extractor      extract.Config
	rng            *rand.Rand

	// extractMu serializes extractions (the Rust extractor task and the
	// D-Bus ExtractColors method both reach extract_colors); it also
	// guards lastExtracted.
	extractMu     sync.Mutex
	lastExtracted string

	changes   feed.Tick // monitors
	extracted feed.Tick // an extraction finished (or had nothing to do)

	cycleKick   chan struct{} // cycling config changed: re-render, restart the timer
	extractKick chan bool     // extract; true resets lastExtracted first
}

// New builds a service with no monitors and cycling stopped.
func New(opts Options) *Service {
	rng := opts.Rand
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // wallpaper order, not security
	}
	return &Service{
		monitors:       map[string]MonitorState{},
		sharedCycle:    opts.SharedCycle,
		themingMonitor: opts.ThemingMonitor,
		extractor:      opts.Extractor,
		rng:            rng,
		cycleKick:      make(chan struct{}, 1),
		extractKick:    make(chan bool, 1),
	}
}

// Changes ticks whenever the per-monitor state changes; read Monitors
// for the new value. The returned function unsubscribes.
func (s *Service) Changes() (<-chan struct{}, func()) { return s.changes.Subscribe() }

// Extracted ticks after every color extraction pass, including passes
// that found nothing new to extract (watch_extraction): theme providers
// re-read the palette cache on it.
func (s *Service) Extracted() (<-chan struct{}, func()) { return s.extracted.Subscribe() }

// Monitors snapshots the per-monitor state.
func (s *Service) Monitors() map[string]MonitorState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.monitors)
}

// MonitorNames lists the registered monitors, sorted.
func (s *Service) MonitorNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(maps.Keys(s.monitors))
}

// Wallpaper is a monitor's wallpaper; false when the monitor is
// unknown or has none.
func (s *Service) Wallpaper(monitor string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.monitors[monitor]
	return st.Wallpaper, ok && st.Wallpaper != ""
}

// FitModeOf is a monitor's fit mode; Fill and false when it is
// unknown.
func (s *Service) FitModeOf(monitor string) (FitMode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.monitors[monitor]
	if !ok {
		return FitFill, false
	}
	return st.FitMode, true
}

// CyclingConfig snapshots the active cycling config; nil when stopped.
func (s *Service) CyclingConfig() *Cycling {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cycling.clone()
}

// ThemingMonitor is the configured theming monitor, empty for the
// fallback.
func (s *Service) ThemingMonitor() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.themingMonitor
}

// updateMonitors applies fn to a copy of the monitor map under s.mu
// (fn may read the other fields and use s.rng, but must not lock) and
// publishes it when it changed (Property::set's equality check).
func (s *Service) updateMonitors(fn func(map[string]MonitorState)) {
	s.mu.Lock()
	next := maps.Clone(s.monitors)
	fn(next)
	changed := !maps.Equal(next, s.monitors)
	if changed {
		s.monitors = next
	}
	s.mu.Unlock()
	if changed {
		feed.Notify(&s.changes)
		s.kickExtract(false)
	}
}

func (s *Service) kickExtract(reset bool) {
	for {
		select {
		case s.extractKick <- reset:
			return
		default:
		}
		// A pending kick absorbs this one; keep the stronger reset.
		select {
		case pending := <-s.extractKick:
			reset = reset || pending
		default:
		}
	}
}

func kick(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// SetWallpaper sets path on one monitor, or every monitor when monitor
// is empty. An unknown monitor is ignored; a missing file is an
// ImageNotFoundError.
func (s *Service) SetWallpaper(path, monitor string) error {
	if !exists(path) {
		return &ImageNotFoundError{Path: path}
	}
	s.updateMonitors(func(m map[string]MonitorState) {
		for name, st := range m {
			if monitor == "" || name == monitor {
				st.Wallpaper = path
				m[name] = st
			}
		}
	})
	return nil
}

// SetFitMode sets the mode on one monitor, or every monitor when
// monitor is empty; an unknown monitor is ignored.
func (s *Service) SetFitMode(mode FitMode, monitor string) {
	s.updateMonitors(func(m map[string]MonitorState) {
		for name, st := range m {
			if monitor == "" || name == monitor {
				st.FitMode = mode
				m[name] = st
			}
		}
	})
}

// StartCycling scans directory and starts cycling every monitor
// through it. Sequential starts every monitor at the first image;
// shuffle gives each its own random start, or one shared start with
// SharedCycle. The images are applied at once.
func (s *Service) StartCycling(directory string, interval time.Duration, mode CyclingMode) error {
	s.mu.Lock()
	cfg, err := newCycling(directory, mode, interval, s.rng)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.updateMonitors(func(m map[string]MonitorState) {
		shared := s.rng.IntN(cfg.ImageCount())
		for _, name := range slices.Sorted(maps.Keys(m)) {
			st := m[name]
			switch {
			case mode == Sequential:
				st.CycleIndex = 0
			case s.sharedCycle:
				st.CycleIndex = shared
			default:
				st.CycleIndex = s.rng.IntN(cfg.ImageCount())
			}
			m[name] = st
		}
	})
	s.setCycling(cfg, true)
	return nil
}

// StopCycling stops cycling; the monitors keep their current images.
func (s *Service) StopCycling() { s.setCycling(nil, false) }

// SetCyclingInterval changes the interval of an active cycle, which
// restarts the timer.
func (s *Service) SetCyclingInterval(interval time.Duration) {
	s.mu.Lock()
	if s.cycling == nil {
		s.mu.Unlock()
		return
	}
	next := s.cycling.clone()
	s.mu.Unlock()
	next.Interval = interval
	s.setCycling(next, false)
}

// setCycling publishes a cycling config. A change (or force) renders
// the current indices and restarts the timer in Run
// (handle_cycling_change).
func (s *Service) setCycling(cfg *Cycling, force bool) {
	s.mu.Lock()
	changed := force || !s.cycling.equal(cfg)
	s.cycling = cfg
	s.mu.Unlock()
	if !changed {
		return
	}
	if cfg != nil {
		s.renderCycle()
	}
	kick(s.cycleKick)
}

// renderCycle stores each monitor's current cycle image as its
// wallpaper (render_cycle / render_current).
func (s *Service) renderCycle() {
	s.step(func(*MonitorState, int) {})
}

// step moves every monitor through the cycle and renders its image; a
// no-op without cycling or with an empty pool.
func (s *Service) step(move func(*MonitorState, int)) {
	s.updateMonitors(func(m map[string]MonitorState) {
		cfg := s.cycling
		if cfg == nil || cfg.ImageCount() == 0 {
			return
		}
		// Sorted, so a seeded rng hands out the same indices every run.
		for _, name := range slices.Sorted(maps.Keys(m)) {
			st := m[name]
			move(&st, cfg.ImageCount())
			if img, ok := cfg.ImageAt(st.CycleIndex); ok {
				st.Wallpaper = img
			}
			m[name] = st
		}
	})
}

// Advance moves every monitor to its next cycle image; a no-op without
// cycling.
func (s *Service) Advance() { s.step((*MonitorState).advance) }

// Rewind moves every monitor to its previous cycle image; a no-op
// without cycling.
func (s *Service) Rewind() { s.step((*MonitorState).previous) }

// SetSharedCycle toggles shared shuffle cycling. In an active shuffle
// cycle, turning it on moves every monitor to one index and off gives
// each a random one (handle_shared_cycle_change).
func (s *Service) SetSharedCycle(shared bool) {
	s.mu.Lock()
	if s.sharedCycle == shared {
		s.mu.Unlock()
		return
	}
	s.sharedCycle = shared
	cfg := s.cycling.clone()
	s.mu.Unlock()
	if cfg == nil || cfg.Mode != Shuffle || cfg.ImageCount() == 0 {
		return
	}
	s.updateMonitors(func(m map[string]MonitorState) {
		names := slices.Sorted(maps.Keys(m))
		if len(names) == 0 {
			return
		}
		target := m[names[0]].CycleIndex
		for _, name := range names {
			st := m[name]
			if shared {
				st.CycleIndex = target
			} else {
				st.CycleIndex = s.rng.IntN(cfg.ImageCount())
			}
			if img, ok := cfg.ImageAt(st.CycleIndex); ok {
				st.Wallpaper = img
			}
			m[name] = st
		}
	})
}

// SetThemingMonitor picks the monitor whose wallpaper drives color
// extraction; empty falls back to the lowest-named monitor. A change
// re-extracts.
func (s *Service) SetThemingMonitor(monitor string) {
	s.mu.Lock()
	changed := s.themingMonitor != monitor
	s.themingMonitor = monitor
	s.mu.Unlock()
	if changed {
		s.kickExtract(true)
	}
}

// SetExtractor replaces the extraction tool config; a change
// re-extracts.
func (s *Service) SetExtractor(cfg extract.Config) {
	s.mu.Lock()
	changed := s.extractor != cfg
	s.extractor = cfg
	s.mu.Unlock()
	if changed {
		s.kickExtract(true)
	}
}

// RegisterMonitor adds a monitor. While cycling it starts at a
// distinct index (0 in sequential mode, the shared index or a random
// one in shuffle) showing that image; otherwise it starts with no
// wallpaper. Registering a known monitor is a no-op.
func (s *Service) RegisterMonitor(monitor string) {
	s.updateMonitors(func(m map[string]MonitorState) {
		if _, ok := m[monitor]; ok {
			return
		}
		st := MonitorState{FitMode: FitFill, CycleIndex: s.startingIndexLocked(m)}
		if s.cycling != nil {
			st.Wallpaper, _ = s.cycling.ImageAt(st.CycleIndex)
		}
		m[monitor] = st
	})
}

// startingIndexLocked is new_monitor_starting_index; s.mu is held. The
// shared index is the lowest-named monitor's, where Rust took whichever
// the map iterated first.
func (s *Service) startingIndexLocked(m map[string]MonitorState) int {
	cfg := s.cycling
	if cfg == nil || cfg.ImageCount() == 0 || cfg.Mode == Sequential {
		return 0
	}
	if s.sharedCycle {
		names := slices.Sorted(maps.Keys(m))
		if len(names) == 0 {
			return 0
		}
		return m[names[0]].CycleIndex
	}
	return s.rng.IntN(cfg.ImageCount())
}

// UnregisterMonitor drops a monitor; unknown names are ignored.
func (s *Service) UnregisterMonitor(monitor string) {
	s.updateMonitors(func(m map[string]MonitorState) { delete(m, monitor) })
}

// themingPath resolves the wallpaper extraction reads: the configured
// theming monitor's, or the lowest-named monitor's (theming_fallback).
// skip is true while a configured theming monitor is unplugged: theming
// must not hop to another monitor for the length of a dock cycle.
func themingPath(monitors map[string]MonitorState, theming string) (path string, skip bool) {
	if theming != "" {
		st, ok := monitors[theming]
		if !ok {
			return "", true
		}
		return st.Wallpaper, false
	}
	names := slices.Sorted(maps.Keys(monitors))
	if len(names) == 0 {
		return "", false
	}
	return monitors[names[0]].Wallpaper, false
}

// ExtractColors extracts the palette from the theming wallpaper unless
// it was already extracted (extract_colors). Every call ends with an
// Extracted tick, whether or not a tool ran.
func (s *Service) ExtractColors(ctx context.Context) error {
	return s.extract(ctx, false)
}

func (s *Service) extract(ctx context.Context, reset bool) error {
	s.extractMu.Lock()
	defer s.extractMu.Unlock()
	defer feed.Notify(&s.extracted)
	if reset {
		s.lastExtracted = ""
	}
	s.mu.Lock()
	path, skip := themingPath(s.monitors, s.themingMonitor)
	cfg := s.extractor
	s.mu.Unlock()
	if skip || path == s.lastExtracted {
		return nil
	}
	s.lastExtracted = path
	if path == "" {
		return nil
	}
	return extract.Extract(ctx, cfg, path)
}

// Run drives the service until ctx ends: the cycling timer and
// directory watcher (tasks/cycle_runner.rs) and the extraction loop
// (spawn_color_extractor), which extracts once at start.
func (s *Service) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { s.runCycling(ctx) })
	wg.Go(func() { s.runExtraction(ctx) })
	wg.Wait()
}

func (s *Service) runExtraction(ctx context.Context) {
	reset := true
	for {
		if err := s.extract(ctx, reset); err != nil && ctx.Err() == nil {
			log.Printf("wallpaper: cannot extract colors: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case reset = <-s.extractKick:
		}
	}
}

func (s *Service) runCycling(ctx context.Context) {
	var (
		timer   *time.Timer
		fire    <-chan time.Time
		watcher *dirWatcher
		watched string
	)
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
		}
		timer, fire = nil, nil
	}
	schedule := func(d time.Duration) {
		stopTimer()
		timer = time.NewTimer(d)
		fire = timer.C
	}
	defer func() {
		stopTimer()
		watcher.close()
	}()
	// A cycle started before Run needs its timer too.
	kick(s.cycleKick)
	for {
		var dirEvents <-chan struct{}
		if watcher != nil {
			dirEvents = watcher.events
		}
		select {
		case <-ctx.Done():
			return
		case <-s.cycleKick:
			cfg := s.CyclingConfig()
			if cfg == nil {
				stopTimer()
				watcher.close()
				watcher, watched = nil, ""
				continue
			}
			schedule(cfg.Interval)
			if watched != cfg.Directory {
				watcher.close()
				watcher = watchDirectory(cfg.Directory)
				watched = cfg.Directory
			}
		case <-fire:
			timer, fire = nil, nil
			if next, ok := s.tick(); ok {
				schedule(next)
			}
		case <-dirEvents:
			s.refreshCycling(watched)
		}
	}
}

// tick is handle_timer_fired: advance (or, in independent shuffle,
// jump to a random index), render, and report the next interval. An
// empty pool stops the timer until the pool changes.
func (s *Service) tick() (time.Duration, bool) {
	cfg := s.CyclingConfig()
	if cfg == nil || cfg.ImageCount() == 0 {
		return 0, false
	}
	s.step(func(st *MonitorState, count int) {
		if cfg.Mode == Shuffle && !s.sharedCycle {
			st.CycleIndex = s.rng.IntN(count)
		} else {
			st.advance(count)
		}
	})
	return cfg.Interval, true
}

// refreshCycling rescans the watched directory into the active pool
// (handle_directory_changed).
func (s *Service) refreshCycling(directory string) {
	cfg := s.CyclingConfig()
	if cfg == nil || cfg.Directory != directory {
		return
	}
	s.mu.Lock()
	next, err := cfg.refreshed(s.rng)
	s.mu.Unlock()
	if err != nil {
		log.Printf("wallpaper: cannot refresh cycling images: %v", err)
		return
	}
	s.setCycling(next, false)
}
