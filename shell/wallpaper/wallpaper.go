// Package wallpaper renders the wallpaper service natively
// (crates/wayle-shell/src/shell/wallpaper, bootstrap/wallpaper.rs, and
// the hotplug half of watchers/wallpaper.rs): one Background layer
// surface per output, reconciled against the service's per-monitor
// state, each swapping images with the wallpaper transition. wayle is
// the wallpaper provider; no swww or other tool is involved.
package wallpaper

import (
	"context"
	"image"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/imagedecode"
	"github.com/stubbedev/wayle/service/wallpaper"
	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

// frameInterval paces transition steps.
const frameInterval = 16 * time.Millisecond

// Shell owns the wallpaper service and its surfaces. Its methods run
// on the gelm loop goroutine.
type Shell struct {
	app      *app.Application
	svc      *wallpaper.Service
	cfg      config.WallpaperConfig
	trans    Transition
	outputs  map[string]*app.Output
	surfaces map[string]*surface
}

// Start builds the service from config (build_wallpaper_service),
// registers the outputs that have a connector name, serves
// com.wayle.Wallpaper1 on conn, and starts the cycling and extraction
// loops and the surfaces. It fails like the Rust service init when
// the D-Bus name is taken (another shell is running): no wallpaper is
// drawn then. The returned stop function ends the loops and releases
// the name.
func Start(application *app.Application, outputs []*app.Output, cfg *config.Config, conn *dbus.Conn, trans Transition) (*Shell, func(), error) {
	svc := wallpaper.New(wallpaper.Options{
		Extractor:      extract.FromConfig(cfg.Styling.ColorExtractor),
		ThemingMonitor: cfg.Styling.ColorExtractor.ThemingMonitor,
		SharedCycle:    cfg.Wallpaper.CyclingSameImage,
	})
	release, err := wallpaper.Export(conn, svc)
	if err != nil {
		return nil, nil, err
	}
	s := &Shell{
		app: application, svc: svc, cfg: cfg.Wallpaper, trans: trans,
		outputs: map[string]*app.Output{}, surfaces: map[string]*surface{},
	}
	for _, out := range outputs {
		if out.Name != "" {
			s.outputs[out.Name] = out
			svc.RegisterMonitor(out.Name)
		}
	}
	Bootstrap(svc, cfg.Wallpaper)

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Run(ctx)
	changes, unsubscribe := svc.Changes()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				application.Invoke(s.reconcile)
			}
		}
	}()
	s.reconcile()
	return s, func() {
		cancel()
		unsubscribe()
		release()
	}, nil
}

// OutputWatcher is the session's output hotplug feed: the identity of
// a monitor once its connector name is known, and its removal.
type OutputWatcher interface {
	WatchOutputIdentity(fn func(*app.Output)) (stop func())
	WatchOutputs(added, removed func(*app.Output)) (stop func())
}

// Launch is Start on the session bus, following hotplugged outputs
// through watch. A failure is logged and leaves the shell without
// wallpapers (nil).
func Launch(application *app.Application, outputs []*app.Output, cfg *config.Config, watch OutputWatcher) (*Shell, func()) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("wallpaper: cannot initialize wallpaper service: D-Bus connection failed: %v", err)
		return nil, func() {}
	}
	s, stop, err := Start(application, outputs, cfg, conn, DefaultTransition())
	if err != nil {
		log.Printf("wallpaper: cannot initialize wallpaper service: %v", err)
		return nil, func() {}
	}
	stopIdentity := watch.WatchOutputIdentity(s.AddOutput)
	stopRemoved := watch.WatchOutputs(nil, s.RemoveOutput)
	return s, func() {
		stopIdentity()
		stopRemoved()
		stop()
	}
}

// Service exposes the running service (theme providers subscribe to
// its Extracted ticks).
func (s *Shell) Service() *wallpaper.Service { return s.svc }

// Bootstrap applies the config to freshly registered monitors: the
// global fit mode, then cycling when a directory is set (else the
// single image), then the [[wallpaper.monitors]] entries on top.
//
// The Rust bootstrap leaves the service fit at fill and has the
// surfaces read fit-mode from config instead; seeding it here keeps the
// service, the D-Bus getters, and the pixels in agreement.
func Bootstrap(svc *wallpaper.Service, cfg config.WallpaperConfig) {
	svc.SetFitMode(cfg.FitMode, "")
	cycling := false
	if cfg.CyclingDirectory != "" {
		interval := time.Duration(cfg.CyclingIntervalMins) * time.Minute
		if err := svc.StartCycling(cfg.CyclingDirectory, interval, cfg.CyclingMode); err != nil {
			log.Printf("wallpaper: could not start wallpaper cycling from config: %v", err)
		} else {
			cycling = true
		}
	}
	if !cycling && cfg.Wallpaper != "" {
		if err := svc.SetWallpaper(cfg.Wallpaper, ""); err != nil {
			log.Printf("wallpaper: cannot apply single-file wallpaper from config: %v", err)
		}
	}
	registered := svc.Monitors()
	for _, m := range cfg.Monitors {
		if _, ok := registered[m.Name]; m.Name == "" || !ok {
			continue
		}
		svc.SetFitMode(m.FitMode, m.Name)
		if m.Wallpaper == "" {
			continue
		}
		if err := svc.SetWallpaper(m.Wallpaper, m.Name); err != nil {
			log.Printf("wallpaper: wallpaper path not found for %s: %s", m.Name, m.Wallpaper)
		}
	}
}

// hotplugFitMode is the fit mode for a monitor appearing after
// startup: its own entry, else the global fit-mode.
func hotplugFitMode(cfg config.WallpaperConfig, name string) wallpaper.FitMode {
	if m, ok := cfg.Monitor(name); ok {
		return m.FitMode
	}
	return cfg.FitMode
}

// hotplugWallpaper is the image for a monitor appearing after startup;
// false leaves the service state alone: registration already seeded
// it from an active cycle, or no image is configured at all.
func hotplugWallpaper(cfg config.WallpaperConfig, name string, cycling bool) (string, bool) {
	if cycling {
		return "", false
	}
	path := cfg.Wallpaper
	if m, ok := cfg.Monitor(name); ok && m.Wallpaper != "" {
		path = m.Wallpaper
	}
	return path, path != ""
}

// AddOutput registers an output once its connector name is known and
// applies the config a hotplugged monitor gets (spawn_hotplug_watcher).
func (s *Shell) AddOutput(out *app.Output) {
	if out.Name == "" {
		return
	}
	s.outputs[out.Name] = out
	if _, known := s.svc.Monitors()[out.Name]; !known {
		s.svc.RegisterMonitor(out.Name)
		s.svc.SetFitMode(hotplugFitMode(s.cfg, out.Name), out.Name)
		if path, ok := hotplugWallpaper(s.cfg, out.Name, s.svc.CyclingConfig() != nil); ok {
			if err := s.svc.SetWallpaper(path, out.Name); err != nil {
				log.Printf("wallpaper: cannot apply wallpaper to new monitor %s: %v", out.Name, err)
			}
		}
	}
	s.reconcile()
}

// RemoveOutput drops an unplugged output from the service.
func (s *Shell) RemoveOutput(out *app.Output) {
	if out.Name == "" || s.outputs[out.Name] != out {
		return
	}
	delete(s.outputs, out.Name)
	s.svc.UnregisterMonitor(out.Name)
	s.reconcile()
}

// resolve is the image and fit a monitor renders (reconcile): its own
// state, else its [[wallpaper.monitors]] image, else the global one.
func resolve(cfg config.WallpaperConfig, name string, st wallpaper.MonitorState) (string, wallpaper.FitMode) {
	path := st.Wallpaper
	if path == "" {
		if m, ok := cfg.Monitor(name); ok {
			path = m.Wallpaper
		}
	}
	if path == "" {
		path = cfg.Wallpaper
	}
	return path, st.FitMode
}

// reconcile creates, updates, and removes surfaces to match the
// service state and the known outputs (keeps_surface: both sides must
// know a monitor for its surface to live).
func (s *Shell) reconcile() {
	monitors := s.svc.Monitors()
	for name, surf := range s.surfaces {
		_, registered := monitors[name]
		if out, ok := s.outputs[name]; !registered || !ok || surf.out != out {
			surf.close()
			delete(s.surfaces, name)
		}
	}
	for name, st := range monitors {
		out, ok := s.outputs[name]
		if !ok {
			continue
		}
		surf, ok := s.surfaces[name]
		if !ok {
			var err error
			if surf, err = s.newSurface(name, out); err != nil {
				log.Printf("wallpaper: surface for %s: %v", name, err)
				continue
			}
			s.surfaces[name] = surf
		}
		if path, fit := resolve(s.cfg, name, st); path != "" {
			surf.render(path, fit)
		}
	}
}

// surface is one output's Background layer.
type surface struct {
	shell *Shell
	out   *app.Output
	layer *app.LayerWindow
	view  *view
	// path and fit are what was last asked for; gen invalidates a
	// decode that a newer request overtook.
	path     string
	fit      wallpaper.FitMode
	gen      uint64
	stopAnim func()
}

func (s *Shell) newSurface(name string, out *app.Output) (*surface, error) {
	surf := &surface{shell: s, out: out, view: newView()}
	layer, err := s.app.NewLayer(app.LayerConfig{
		Output:        out,
		Layer:         app.LayerBackground,
		Anchor:        app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight,
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardNone,
		Namespace:     "wayle-wallpaper",
		Root:          surf.view,
		Background:    backdrop,
		OnClosed: func() {
			// The output went away (or the compositor closed us): the
			// next reconcile rebuilds it if the monitor is still known.
			if s.surfaces[name] == surf {
				delete(s.surfaces, name)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	surf.layer = layer
	return surf, nil
}

func (surf *surface) close() {
	if surf.stopAnim != nil {
		surf.stopAnim()
	}
	surf.gen++
	surf.layer.Close()
}

// outputTarget is the output's device-pixel size and scale; false when
// the compositor has not reported them yet.
func outputTarget(out *app.Output) (target, bool) {
	w, h := out.ModeW, out.ModeH
	if out.Transform%2 == 1 { // 90/270, flipped or not
		w, h = h, w
	}
	if w <= 0 || h <= 0 {
		return target{}, false
	}
	scale := float64(max(out.Scale, 1))
	if out.LogicalW > 0 {
		scale = float64(w) / float64(out.LogicalW)
	}
	return target{w: w, h: h, scale: scale}, true
}

// render decodes path off the loop and swaps it in. A repeat of the
// current request is a no-op; a fit change alone re-renders without a
// transition.
func (surf *surface) render(path string, fit wallpaper.FitMode) {
	if path == surf.path && fit == surf.fit {
		return
	}
	animate := path != surf.path
	surf.path, surf.fit = path, fit
	surf.gen++
	gen := surf.gen
	box, known := outputTarget(surf.out)
	application := surf.shell.app
	go func() {
		img, err := imagedecode.Open(path)
		if err != nil {
			log.Printf("wallpaper: cannot decode wallpaper %s: %v", path, err)
			return
		}
		scale := widget.ImageScaleDown
		if known {
			img = prepare(img, fit, box)
		} else {
			scale = widgetScale(fit)
		}
		application.Invoke(func() {
			if gen != surf.gen {
				return
			}
			surf.swap(img, scale, animate)
		})
	}()
}

func (surf *surface) swap(img image.Image, scale widget.ImageScale, animate bool) {
	if surf.stopAnim != nil {
		surf.stopAnim()
		surf.stopAnim = nil
	}
	im := widget.NewImage(img)
	im.SetScale(scale)
	trans := surf.shell.trans
	kind := trans.Kind
	if !animate || trans.Duration <= 0 {
		kind = TransitionNone
	}
	surf.view.show(im, img.Bounds().Size(), kind)
	if kind == TransitionNone {
		return
	}
	start := time.Now()
	surf.stopAnim = surf.shell.app.Every(frameInterval, func() {
		t := float64(time.Since(start)) / float64(trans.Duration)
		if !surf.view.step(t) && surf.stopAnim != nil {
			surf.stopAnim()
			surf.stopAnim = nil
		}
	})
}
