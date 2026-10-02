package wallpaper

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/wallpaper"
	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestPrepareFits(t *testing.T) {
	src := solid(400, 200, color.NRGBA{R: 255, A: 255})
	box := target{w: 100, h: 100, scale: 1}
	for _, tt := range []struct {
		fit          wallpaper.FitMode
		wantW, wantH int
	}{
		{wallpaper.FitFill, 100, 100},    // cover: crop to the box
		{wallpaper.FitFit, 100, 50},      // contain: letterbox
		{wallpaper.FitStretch, 100, 100}, // ignore the aspect
		{wallpaper.FitCenter, 100, 50},   // too big: shrink to fit
	} {
		got := prepare(src, tt.fit, box).Bounds()
		if got.Dx() != tt.wantW || got.Dy() != tt.wantH {
			t.Errorf("%v: %dx%d, want %dx%d", tt.fit, got.Dx(), got.Dy(), tt.wantW, tt.wantH)
		}
	}
}

func TestPrepareCenterIsNaturalLogicalSize(t *testing.T) {
	small := solid(20, 10, color.NRGBA{G: 255, A: 255})
	// At scale 1 a fitting image is untouched, not enlarged.
	if got := prepare(small, wallpaper.FitCenter, target{w: 100, h: 100, scale: 1}); got != image.Image(small) {
		t.Errorf("scale 1 center resampled to %v", got.Bounds())
	}
	// At scale 2 the natural size is 20x10 logical = 40x20 device, as GTK
	// measures a texture.
	if got := prepare(small, wallpaper.FitCenter, target{w: 100, h: 100, scale: 2}).Bounds(); got.Dx() != 40 || got.Dy() != 20 {
		t.Errorf("scale 2 center = %v, want 40x20", got)
	}
	// An exact 1:1 cover needs no resample either.
	exact := solid(100, 100, color.NRGBA{A: 255})
	if got := prepare(exact, wallpaper.FitFill, target{w: 100, h: 100, scale: 1}); got != image.Image(exact) {
		t.Error("a 1:1 cover was resampled")
	}
}

func TestWidgetScaleFallback(t *testing.T) {
	for fit, want := range map[wallpaper.FitMode]widget.ImageScale{
		wallpaper.FitFill: widget.ImageCover, wallpaper.FitFit: widget.ImageFit,
		wallpaper.FitStretch: widget.ImageStretch, wallpaper.FitCenter: widget.ImageScaleDown,
	} {
		if got := widgetScale(fit); got != want {
			t.Errorf("%v -> %v, want %v", fit, got, want)
		}
	}
}

func TestOutputTarget(t *testing.T) {
	out := &app.Output{ModeW: 3840, ModeH: 2160, Scale: 2, LogicalW: 2560, LogicalH: 1440}
	if got, ok := outputTarget(out); !ok || got.w != 3840 || got.h != 2160 || got.scale != 1.5 {
		t.Errorf("fractional = %+v %v, want 3840x2160 at 1.5", got, ok)
	}
	rotated := &app.Output{ModeW: 1920, ModeH: 1080, Transform: 1, Scale: 1}
	if got, _ := outputTarget(rotated); got.w != 1080 || got.h != 1920 || got.scale != 1 {
		t.Errorf("rotated = %+v, want 1080x1920", got)
	}
	if _, ok := outputTarget(&app.Output{}); ok {
		t.Error("an output without a mode has a target")
	}
}

func TestTransitionFor(t *testing.T) {
	for in, want := range map[string]TransitionKind{
		"none": TransitionNone, "fade": TransitionCrossfade, "slide-up": TransitionSlideUp,
		"slide-down": TransitionSlideDown, "slide-left": TransitionSlideLeft, "slide-right": TransitionSlideRight,
		"genie": TransitionCrossfade, "swing-up": TransitionCrossfade, "flip": TransitionCrossfade,
	} {
		got, err := TransitionFor(in, time.Second)
		if err != nil || got.Kind != want || got.Duration != time.Second {
			t.Errorf("%s = %+v %v, want %v", in, got, err, want)
		}
	}
	if _, err := TransitionFor("slide_up", time.Second); err == nil {
		t.Error("an unknown animation type mapped")
	}
	if d := DefaultTransition(); d.Kind != TransitionCrossfade || d.Duration != 200*time.Millisecond {
		t.Errorf("default = %+v", d)
	}
}

func paintView(v *view, w, h int) ([]byte, int) {
	data := make([]byte, render.Stride(w)*h)
	cv := render.NewScaled(data, render.Stride(w), w, h, 1, 1)
	cv.Clear(cv.Rect(), render.RGB(0, 0, 0))
	v.Measure(widget.Constraints{Max: widget.Size{W: w, H: h}})
	v.Arrange(render.Rect{W: w, H: h})
	v.Paint(cv)
	return data, w
}

func px(data []byte, w, x, y int) render.Color {
	return render.ColorFromBytes(data[y*render.Stride(w)+x*4:])
}

func imageOf(c color.NRGBA, w, h int) (*widget.Image, image.Point) {
	im := widget.NewImage(solid(w, h, c))
	im.SetScale(widget.ImageScaleDown)
	return im, image.Pt(w, h)
}

func TestViewCrossfadeBlendsAndFinishes(t *testing.T) {
	v := newView()
	red, redSize := imageOf(color.NRGBA{R: 255, A: 255}, 4, 4)
	v.show(red, redSize, TransitionCrossfade)
	if v.prev != nil || v.progress != 1 {
		t.Fatal("the first image transitioned from nothing")
	}
	blue, blueSize := imageOf(color.NRGBA{B: 255, A: 255}, 4, 4)
	v.show(blue, blueSize, TransitionCrossfade)
	if !v.step(0.5) {
		t.Fatal("the transition ended halfway")
	}
	data, w := paintView(v, 4, 4)
	mid := px(data, w, 1, 1)
	if mid.R() == 0 || mid.B() == 0 {
		t.Errorf("mid-crossfade pixel = %v, want red and blue mixed", mid)
	}
	if v.step(1) || v.prev != nil {
		t.Error("the transition did not finish at t=1")
	}
	data, w = paintView(v, 4, 4)
	if got := px(data, w, 1, 1); got != render.RGB(0, 0, 0xff) {
		t.Errorf("finished pixel = %v, want blue", got)
	}
}

func TestViewCrossfadeFadesTheOldImageOutOfTheLetterbox(t *testing.T) {
	v := newView()
	red, redSize := imageOf(color.NRGBA{R: 255, A: 255}, 4, 4)
	v.show(red, redSize, TransitionNone)
	// A narrow incoming image leaves a letterbox at x=0.
	blue, blueSize := imageOf(color.NRGBA{B: 255, A: 255}, 2, 4)
	v.show(blue, blueSize, TransitionCrossfade)
	v.step(0.99)
	data, w := paintView(v, 4, 4)
	if r := px(data, w, 0, 0).R(); r > 0x20 {
		t.Errorf("letterbox pixel = %v: the outgoing image stays at full strength", px(data, w, 0, 0))
	}
}

func TestViewNoneSwapsAtOnce(t *testing.T) {
	v := newView()
	a, as := imageOf(color.NRGBA{R: 255, A: 255}, 2, 2)
	v.show(a, as, TransitionNone)
	b, bs := imageOf(color.NRGBA{G: 255, A: 255}, 2, 2)
	v.show(b, bs, TransitionNone)
	if v.prev != nil || v.progress != 1 {
		t.Error("a none transition kept the old image")
	}
}

func TestViewSlideOffsets(t *testing.T) {
	v := newView()
	v.kind, v.progress = TransitionSlideLeft, 0
	r := render.Rect{W: 100, H: 50}
	if inX, _, outX, _ := v.offsets(r); inX != 100 || outX != 0 {
		t.Errorf("slide-left start = in %d out %d", inX, outX)
	}
	v.progress = 1
	if inX, _, outX, _ := v.offsets(r); inX != 0 || outX != -100 {
		t.Errorf("slide-left end = in %d out %d", inX, outX)
	}
	v.kind, v.progress = TransitionSlideDown, 0
	if _, inY, _, outY := v.offsets(r); inY != -50 || outY != 0 {
		t.Errorf("slide-down start = in %d out %d", inY, outY)
	}
	v.kind = TransitionCrossfade
	if a, b, c, d := v.offsets(r); a|b|c|d != 0 {
		t.Error("a crossfade moved the images")
	}
}

func TestEaseOutCubic(t *testing.T) {
	if easeOutCubic(0) != 0 || easeOutCubic(1) != 1 || easeOutCubic(0.5) <= 0.5 {
		t.Errorf("ease = %v %v %v", easeOutCubic(0), easeOutCubic(0.5), easeOutCubic(1))
	}
}

func wcfg(monitors ...config.MonitorWallpaperConfig) config.WallpaperConfig {
	c := config.DefaultsWallpaper()
	c.Monitors = monitors
	return c
}

func TestHotplugWallpaper(t *testing.T) {
	entry := config.MonitorWallpaperConfig{Name: "DP-1", Wallpaper: "mine.png"}
	global := wcfg(entry)
	global.Wallpaper = "global.png"
	if p, ok := hotplugWallpaper(global, "DP-1", false); !ok || p != "mine.png" {
		t.Errorf("override = %q %v", p, ok)
	}
	if p, ok := hotplugWallpaper(global, "HDMI-1", false); !ok || p != "global.png" {
		t.Errorf("global fallback = %q %v", p, ok)
	}
	fitOnly := wcfg(config.MonitorWallpaperConfig{Name: "DP-1", FitMode: wallpaper.FitFit})
	fitOnly.Wallpaper = "global.png"
	if p, _ := hotplugWallpaper(fitOnly, "DP-1", false); p != "global.png" {
		t.Errorf("a fit-only entry = %q, want the global image", p)
	}
	// Cycling already seeded the monitor at registration.
	if _, ok := hotplugWallpaper(global, "DP-1", true); ok {
		t.Error("hotplug overwrote a cycling monitor")
	}
	if _, ok := hotplugWallpaper(wcfg(), "DP-1", false); ok {
		t.Error("hotplug applied an image with none configured")
	}
}

func TestHotplugFitMode(t *testing.T) {
	c := wcfg(config.MonitorWallpaperConfig{Name: "DP-1", FitMode: wallpaper.FitCenter})
	c.FitMode = wallpaper.FitStretch
	if got := hotplugFitMode(c, "DP-1"); got != wallpaper.FitCenter {
		t.Errorf("entry fit = %v", got)
	}
	if got := hotplugFitMode(c, "HDMI-1"); got != wallpaper.FitStretch {
		t.Errorf("global fit = %v", got)
	}
}

func TestResolve(t *testing.T) {
	c := wcfg(config.MonitorWallpaperConfig{Name: "DP-1", Wallpaper: "override.png"})
	c.Wallpaper = "global.png"
	st := wallpaper.MonitorState{FitMode: wallpaper.FitFit}
	if p, fit := resolve(c, "DP-1", st); p != "override.png" || fit != wallpaper.FitFit {
		t.Errorf("override = %q %v", p, fit)
	}
	if p, _ := resolve(c, "DP-2", st); p != "global.png" {
		t.Errorf("fallback = %q", p)
	}
	st.Wallpaper = "state.png"
	if p, _ := resolve(c, "DP-1", st); p != "state.png" {
		t.Errorf("state wins = %q", p)
	}
	if p, _ := resolve(wcfg(), "DP-1", wallpaper.MonitorState{}); p != "" {
		t.Errorf("nothing configured = %q", p)
	}
}

func files(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newSvc(monitors ...string) *wallpaper.Service {
	s := wallpaper.New(wallpaper.Options{Extractor: extract.Config{Tool: extract.None}})
	for _, m := range monitors {
		s.RegisterMonitor(m)
	}
	return s
}

func TestBootstrapSingleFileAndOverrides(t *testing.T) {
	dir := files(t, "global.png", "primary.png")
	c := wcfg(
		config.MonitorWallpaperConfig{Name: "DP-1", FitMode: wallpaper.FitCenter, Wallpaper: filepath.Join(dir, "primary.png")},
		config.MonitorWallpaperConfig{Name: "DP-2", FitMode: wallpaper.FitFit, Wallpaper: filepath.Join(dir, "missing.png")},
		config.MonitorWallpaperConfig{Name: "HDMI-9", Wallpaper: filepath.Join(dir, "primary.png")},
	)
	c.Wallpaper = filepath.Join(dir, "global.png")
	c.FitMode = wallpaper.FitStretch
	svc := newSvc("DP-1", "DP-2", "DP-3")
	Bootstrap(svc, c)
	m := svc.Monitors()
	if m["DP-1"].Wallpaper != filepath.Join(dir, "primary.png") || m["DP-1"].FitMode != wallpaper.FitCenter {
		t.Errorf("DP-1 = %+v", m["DP-1"])
	}
	// A missing override keeps the global image but takes the entry's fit.
	if m["DP-2"].Wallpaper != filepath.Join(dir, "global.png") || m["DP-2"].FitMode != wallpaper.FitFit {
		t.Errorf("DP-2 = %+v", m["DP-2"])
	}
	if m["DP-3"].Wallpaper != filepath.Join(dir, "global.png") || m["DP-3"].FitMode != wallpaper.FitStretch {
		t.Errorf("DP-3 = %+v, want the global image and fit", m["DP-3"])
	}
	if _, ok := m["HDMI-9"]; ok {
		t.Error("an entry for an absent monitor registered it")
	}
}

func TestBootstrapCyclingTakesPrecedence(t *testing.T) {
	dir := files(t, "a.png", "b.png")
	single := files(t, "single.png")
	c := wcfg()
	c.CyclingDirectory = dir
	c.Wallpaper = filepath.Join(single, "single.png")
	svc := newSvc("DP-1")
	Bootstrap(svc, c)
	if svc.CyclingConfig() == nil {
		t.Fatal("cycling did not start")
	}
	if got := svc.Monitors()["DP-1"].Wallpaper; got != filepath.Join(dir, "a.png") {
		t.Errorf("DP-1 = %q, want the first cycle image", got)
	}
	// A cycling directory that fails falls back to the single image.
	c.CyclingDirectory = "/nonexistent/wayle"
	svc = newSvc("DP-1")
	Bootstrap(svc, c)
	if svc.CyclingConfig() != nil || svc.Monitors()["DP-1"].Wallpaper != filepath.Join(single, "single.png") {
		t.Errorf("fallback = %+v", svc.Monitors()["DP-1"])
	}
}

// The view clips to its own bounds on a scaled canvas: at 2x a
// half-way slide fills the view's whole device extent and draws
// nothing past it.
func TestViewClipsToItsBoundsAtScale(t *testing.T) {
	v := newView()
	// 8px sources fill the 4px logical view at 2x exactly.
	red, redSize := imageOf(color.NRGBA{R: 255, A: 255}, 8, 8)
	v.show(red, redSize, TransitionNone)
	green, greenSize := imageOf(color.NRGBA{G: 255, A: 255}, 8, 8)
	v.show(green, greenSize, TransitionSlideRight)
	v.step(0.2)    // eased to about half: the outgoing image 2px right
	const dev = 16 // logical 8x8 at 2x; the view holds the top-left 4x4
	data := make([]byte, render.Stride(dev)*dev)
	cv := render.NewScaled(data, render.Stride(dev), dev, dev, 2, 1)
	cv.Clear(cv.Rect(), render.RGB(0, 0, 0))
	v.Measure(widget.Constraints{Max: widget.Size{W: 4, H: 4}})
	v.Arrange(render.Rect{W: 4, H: 4})
	v.Paint(cv)
	// The outgoing image slides out across the view's right edge.
	if got := px(data, dev, 7, 3); got != render.RGB(0xff, 0, 0) {
		t.Errorf("device (7,3) inside the view = %v, want the outgoing image", got)
	}
	for _, x := range []int{8, 10} {
		if got := px(data, dev, x, 3); got != render.RGB(0, 0, 0) {
			t.Errorf("device (%d,2) past the view painted %v", x, got)
		}
	}
}
