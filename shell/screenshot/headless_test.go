package screenshot

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/regionoverlay"
)

// The compositor-in-the-loop half: real captures against a private
// headless compositor (sway on the pixman renderer). It runs only with
// WAYLE_CAPTURE_HEADLESS=1 and WAYLAND_DISPLAY/XDG_RUNTIME_DIR pointing
// at that private session - it must never capture a developer's
// screen.
func requireHeadless(t *testing.T) {
	t.Helper()
	if os.Getenv("WAYLE_CAPTURE_HEADLESS") == "" {
		t.Skip("WAYLE_CAPTURE_HEADLESS is not set")
	}
}

func TestHeadlessCaptureOutput(t *testing.T) {
	requireHeadless(t)
	img, err := captureOutput("")
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("empty capture %v", b)
	}
	if _, err := captureOutput("NOPE-1"); err == nil || !strings.Contains(err.Error(), "output NOPE-1 not found") {
		t.Fatalf("a missing output = %v", err)
	}
	frozen, err := captureAllOutputs()
	if err != nil || len(frozen) == 0 || frozen[0].connector == "" {
		t.Fatalf("captureAllOutputs = %v %v", frozen, err)
	}
	// sway offers ext toplevel capture, and no window matches an
	// identity nothing carries.
	if _, err := captureWindow(windowTarget{appID: "no.such.app", hasAppID: true}); err == nil ||
		!strings.Contains(err.Error(), "could not find the target window") {
		t.Fatalf("captureWindow = %v", err)
	}
}

func TestHeadlessHostRegionSavesTheCrop(t *testing.T) {
	requireHeadless(t)
	frozen, err := captureAllOutputs()
	if err != nil {
		t.Fatal(err)
	}
	b := frozen[0].image.Bounds()
	cfg := config.DefaultsScreenshot()
	cfg.OutputDirectory = t.TempDir()
	h := &Host{
		Config: cfg,
		Monitors: func() []regionoverlay.Monitor {
			return []regionoverlay.Monitor{{Connector: frozen[0].connector, Width: b.Dx(), Height: b.Dy()}}
		},
		Region: &fakeRegion{sel: regionoverlay.Selection{Output: frozen[0].connector, X: 5, Y: 5, Width: 30, Height: 20}, ok: true},
		Notify: func(string) {},
	}
	path, err := h.Capture(ModeRegion, "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 30 || img.Bounds().Dy() != 20 {
		t.Fatalf("saved %v, want the 30x20 crop", img.Bounds())
	}
}
