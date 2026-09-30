package screenshot

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/colorpicker"
	"github.com/stubbedev/wayle/shell/regionoverlay"
)

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

var (
	red   = color.RGBA{255, 0, 0, 255}
	green = color.RGBA{0, 255, 0, 255}
)

func geom(x, y, w, h int) logicalGeometry { return logicalGeometry{x: x, y: y, width: w, height: h} }

func TestLayoutBounds(t *testing.T) {
	cases := []struct {
		name       string
		geoms      []logicalGeometry
		x, y, w, h int
		wantOK     bool
	}{
		{"none", nil, 0, 0, 0, 0, false},
		{"single", []logicalGeometry{geom(0, 0, 1920, 1080)}, 0, 0, 1920, 1080, true},
		{"side by side", []logicalGeometry{geom(0, 0, 1920, 1080), geom(1920, 0, 2560, 1440)}, 0, 0, 4480, 1440, true},
		{"negative origin", []logicalGeometry{geom(0, 0, 1920, 1080), geom(-1280, -200, 1280, 1024)}, -1280, -200, 3200, 1280, true},
		{"stacked with a gap", []logicalGeometry{geom(0, 0, 1000, 500), geom(0, 600, 800, 400)}, 0, 0, 1000, 1000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y, w, h, ok := layoutBounds(tc.geoms)
			if ok != tc.wantOK || x != tc.x || y != tc.y || w != tc.w || h != tc.h {
				t.Fatalf("layoutBounds = %d %d %d %d %v", x, y, w, h, ok)
			}
		})
	}
}

func TestCompositeOutputs(t *testing.T) {
	t.Run("no frames is zero sized", func(t *testing.T) {
		if b := compositeOutputs(nil).Bounds(); !b.Empty() {
			t.Fatalf("bounds %v", b)
		}
	})
	t.Run("HiDPI frames scale to their logical size", func(t *testing.T) {
		img := compositeOutputs([]placedFrame{{logical: geom(0, 0, 100, 100), image: solid(200, 200, red)}})
		if img.Bounds().Dx() != 100 || img.RGBAAt(50, 50) != red {
			t.Fatalf("bounds %v pixel %v", img.Bounds(), img.RGBAAt(50, 50))
		}
	})
	t.Run("side by side outputs land at their offsets", func(t *testing.T) {
		img := compositeOutputs([]placedFrame{
			{logical: geom(0, 0, 192, 108), image: solid(192, 108, red)},
			{logical: geom(192, 0, 256, 144), image: solid(256, 144, green)},
		})
		if img.Bounds().Dx() != 448 || img.Bounds().Dy() != 144 {
			t.Fatalf("bounds %v", img.Bounds())
		}
		if img.RGBAAt(191, 107) != red || img.RGBAAt(192, 0) != green || img.RGBAAt(447, 143) != green {
			t.Fatal("outputs misplaced")
		}
		if img.RGBAAt(0, 120) != (color.RGBA{}) {
			t.Fatal("the gap under the shorter output is not empty")
		}
	})
	t.Run("negative origins map to the canvas top left", func(t *testing.T) {
		img := compositeOutputs([]placedFrame{
			{logical: geom(0, 0, 192, 108), image: solid(192, 108, red)},
			{logical: geom(-128, -20, 128, 102), image: solid(128, 102, green)},
		})
		if img.RGBAAt(0, 0) != green || img.RGBAAt(128, 20) != red {
			t.Fatal("negative origin misplaced")
		}
	})
	t.Run("a zero logical size paints nothing", func(t *testing.T) {
		if b := compositeOutputs([]placedFrame{{logical: geom(0, 0, 0, 0), image: solid(10, 10, red)}}).Bounds(); !b.Empty() {
			t.Fatalf("bounds %v", b)
		}
	})
}

func TestCropFrozen(t *testing.T) {
	// A 2x physical frame: left half red, right half green.
	img := solid(200, 100, red)
	for y := range 100 {
		for x := 100; x < 200; x++ {
			img.SetRGBA(x, y, green)
		}
	}
	t.Run("scales the logical selection to physical pixels", func(t *testing.T) {
		out := cropFrozen(img, 100, 50, regionoverlay.Selection{X: 40, Y: 10, Width: 20, Height: 10})
		if out.Bounds().Dx() != 40 || out.Bounds().Dy() != 20 {
			t.Fatalf("crop %v, want 40x20", out.Bounds())
		}
		if out.RGBAAt(0, 0) != red || out.RGBAAt(39, 19) != green {
			t.Fatal("crop took the wrong pixels")
		}
	})
	t.Run("clamps a selection running off the frame", func(t *testing.T) {
		out := cropFrozen(img, 100, 50, regionoverlay.Selection{X: 90, Y: 40, Width: 50, Height: 50})
		if out.Bounds().Dx() != 20 || out.Bounds().Dy() != 20 {
			t.Fatalf("crop %v, want the 20x20 left inside", out.Bounds())
		}
		out = cropFrozen(img, 100, 50, regionoverlay.Selection{X: 500, Y: 500, Width: 5, Height: 5})
		if !out.Bounds().Empty() {
			t.Fatalf("a selection past the frame cropped %v", out.Bounds())
		}
	})
}

func TestMatchesTarget(t *testing.T) {
	both := windowTarget{appID: "foot", hasAppID: true, title: "shell", hasTitle: true}
	if !matchesTarget("foot", "shell", both) {
		t.Error("exact app id and title did not match")
	}
	if matchesTarget("foot", "other", both) || matchesTarget("kitty", "shell", both) {
		t.Error("a differing key matched")
	}
	appOnly := windowTarget{appID: "foot", hasAppID: true}
	if !matchesTarget("foot", "anything", appOnly) {
		t.Error("an app-id-only target ignored nothing")
	}
	if matchesTarget("foot", "shell", windowTarget{}) {
		t.Error("a target with no identity matched a window")
	}
}

func TestParseHyprlandActiveWindow(t *testing.T) {
	tgt, ok := parseHyprlandActiveWindow(`{"address":"0x55d0a1","class":"foot","title":"~"}`)
	if !ok || !tgt.hasHandle || tgt.hyprlandHandle != 0x55d0a1 || tgt.appID != "foot" || tgt.title != "~" {
		t.Fatalf("parsed %+v %v", tgt, ok)
	}
	if _, ok := parseHyprlandActiveWindow(`{}`); ok {
		t.Error("an empty object parsed as an active window")
	}
	if _, ok := parseHyprlandActiveWindow(`not json`); ok {
		t.Error("garbage parsed as an active window")
	}
	tgt, ok = parseHyprlandActiveWindow(`{"address":"zz","class":"a","title":"b"}`)
	if !ok || tgt.hasHandle {
		t.Errorf("a bad address kept a handle: %+v", tgt)
	}
}

// fakeRegion answers the region overlay.
type fakeRegion struct {
	sel    regionoverlay.Selection
	ok     bool
	frames map[string]*image.RGBA
}

func (f *fakeRegion) Request(frames map[string]*image.RGBA) (regionoverlay.Selection, bool) {
	f.frames = frames
	return f.sel, f.ok
}

type fakePicker struct {
	c  colorpicker.RGB
	ok bool
}

func (f fakePicker) Request(map[string]*image.RGBA) (colorpicker.RGB, bool) { return f.c, f.ok }

func testHost(t *testing.T, region *fakeRegion) (*Host, *[]string, *int) {
	t.Helper()
	cfg := config.DefaultsScreenshot()
	cfg.OutputDirectory = filepath.Join(t.TempDir(), "shots")
	var notified []string
	copies := 0
	h := &Host{
		Config: cfg,
		Monitors: func() []regionoverlay.Monitor {
			return []regionoverlay.Monitor{{Connector: "DP-1", Width: 100, Height: 50}, {Connector: "DP-2", X: 100, Width: 100, Height: 50}}
		},
		Region: region,
		Picker: fakePicker{c: colorpicker.RGB{R: 255, G: 51, B: 0}, ok: true},
		Copy:   func(image.Image) error { copies++; return nil },
		Notify: func(p string) { notified = append(notified, p) },
		now:    func() time.Time { return time.Date(2026, 9, 30, 12, 34, 56, 0, time.Local) },
		frozen: func() ([]frozenOutput, error) {
			return []frozenOutput{{"DP-1", solid(200, 100, red)}, {"DP-2", solid(100, 50, green)}}, nil
		},
	}
	return h, &notified, &copies
}

func TestHostRegionCapture(t *testing.T) {
	region := &fakeRegion{sel: regionoverlay.Selection{Output: "DP-1", X: 10, Y: 10, Width: 20, Height: 10}, ok: true}
	h, notified, copies := testHost(t, region)
	path, err := h.Capture(ModeRegion, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(h.Config.OutputDirectory, "Screenshot_2026-09-30_12-34-56.png")
	if path != want {
		t.Fatalf("path %q, want %q", path, want)
	}
	if len(region.frames) != 2 || region.frames["DP-1"] == nil {
		t.Fatal("the overlay did not get the frozen frames")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 20 {
		t.Fatalf("saved %v, want the 2x-scaled 40x20 crop", b)
	}
	if *copies != 1 || len(*notified) != 1 || (*notified)[0] != path {
		t.Fatalf("copies %d notified %v", *copies, *notified)
	}
}

func TestHostRegionCancel(t *testing.T) {
	h, notified, copies := testHost(t, &fakeRegion{ok: false})
	path, err := h.Capture(ModeRegion, "")
	if err != nil || path != "" {
		t.Fatalf("cancel = %q, %v; want an empty path and no error", path, err)
	}
	if _, err := os.Stat(h.Config.OutputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Error("a cancelled capture created the output directory")
	}
	if *copies != 0 || len(*notified) != 0 {
		t.Error("a cancelled capture copied or notified")
	}
}

func TestHostRegionOnAVanishedOutput(t *testing.T) {
	h, _, _ := testHost(t, &fakeRegion{sel: regionoverlay.Selection{Output: "HDMI-9", Width: 5, Height: 5}, ok: true})
	if _, err := h.Capture(ModeRegion, ""); err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("err = %v", err)
	}
}

func TestHostOptionsOff(t *testing.T) {
	h, notified, copies := testHost(t, &fakeRegion{sel: regionoverlay.Selection{Output: "DP-2", Width: 5, Height: 5}, ok: true})
	h.Config.CopyToClipboard, h.Config.Notify = false, false
	if _, err := h.Capture(ModeRegion, ""); err != nil {
		t.Fatal(err)
	}
	if *copies != 0 || len(*notified) != 0 {
		t.Fatalf("disabled options ran: copies %d notified %v", *copies, *notified)
	}
}

func TestHostScreenComposites(t *testing.T) {
	h, _, _ := testHost(t, &fakeRegion{})
	path, err := h.Capture(ModeScreen, "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 50 {
		t.Fatalf("screen %v, want the 200x50 layout", b)
	}
	h.Monitors = func() []regionoverlay.Monitor { return nil }
	if _, err := h.Capture(ModeScreen, ""); err == nil {
		t.Fatal("a screen capture without monitor geometry succeeded")
	}
}

func TestHostUnknownMode(t *testing.T) {
	h, _, _ := testHost(t, &fakeRegion{})
	if _, err := h.Capture("everything", ""); err == nil || err.Error() != "unknown screenshot mode: everything" {
		t.Fatalf("err = %v", err)
	}
}

func TestHostSaveFailure(t *testing.T) {
	h, _, _ := testHost(t, &fakeRegion{sel: regionoverlay.Selection{Output: "DP-2", Width: 5, Height: 5}, ok: true})
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h.Config.OutputDirectory = filepath.Join(file, "sub")
	if _, err := h.Capture(ModeRegion, ""); err == nil || !strings.HasPrefix(err.Error(), "cannot create") {
		t.Fatalf("err = %v", err)
	}
}

func TestHostPickColor(t *testing.T) {
	h, _, _ := testHost(t, &fakeRegion{})
	r, g, b, err := h.PickColor()
	if err != nil || r != 1 || g != 0.2 || b != 0 {
		t.Fatalf("pick = %v %v %v %v", r, g, b, err)
	}
	h.Picker = fakePicker{ok: false}
	if _, _, _, err := h.PickColor(); err == nil || err.Error() != "color pick cancelled" {
		t.Fatalf("cancel err = %v", err)
	}
	h.frozen = func() ([]frozenOutput, error) { return nil, nil }
	if _, _, _, err := h.PickColor(); err == nil || err.Error() != "no outputs available" {
		t.Fatalf("no outputs err = %v", err)
	}
}

func TestResolveDir(t *testing.T) {
	t.Setenv("XDG_PICTURES_DIR", "/pics")
	t.Setenv("HOME", "/home/u")
	if got := resolveDir("/custom"); got != "/custom" {
		t.Errorf("configured = %q", got)
	}
	if got := resolveDir(""); got != "/pics" {
		t.Errorf("xdg = %q", got)
	}
	t.Setenv("XDG_PICTURES_DIR", "")
	if got := resolveDir(""); got != "/home/u/Pictures" {
		t.Errorf("home = %q", got)
	}
	t.Setenv("HOME", "")
	if got := resolveDir(""); got != "." {
		t.Errorf("fallback = %q", got)
	}
}

// fakeCapturer is the daemon's host in tests.
type fakeCapturer struct{ err error }

func (f fakeCapturer) Capture(mode, target string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "/p/" + mode + target, nil
}

func (f fakeCapturer) PickColor() (float64, float64, float64, error) {
	return 0.5, 0.25, 1, f.err
}

func TestDaemonMethods(t *testing.T) {
	d := &Daemon{host: fakeCapturer{}}
	if p, err := d.Capture("output", "DP-1"); err != nil || p != "/p/outputDP-1" {
		t.Fatalf("Capture = %q %v", p, err)
	}
	if r, g, b, err := d.PickColor(); err != nil || r != 0.5 || g != 0.25 || b != 1 {
		t.Fatalf("PickColor = %v %v %v %v", r, g, b, err)
	}
	d = &Daemon{host: fakeCapturer{err: errors.New("no outputs available")}}
	_, err := d.Capture("output", "")
	if err == nil || err.Name != "org.freedesktop.DBus.Error.Failed" || err.Body[0] != "no outputs available" {
		t.Fatalf("Capture error = %v", err)
	}
	if _, _, _, err := d.PickColor(); err == nil || err.Name != "org.freedesktop.DBus.Error.Failed" {
		t.Fatalf("PickColor error = %v", err)
	}
}
