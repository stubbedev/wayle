package portal

import (
	"os"
	"testing"

	"github.com/stubbedev/gelm/capture"

	"github.com/stubbedev/wayle/internal/pipewire/pwtest"
)

// The real capture-to-PipeWire path against a private headless
// compositor; runs only with WAYLE_CAPTURE_HEADLESS=1 and
// WAYLAND_DISPLAY/XDG_RUNTIME_DIR pointing at that session (it must
// never stream a developer's screen). The PipeWire daemon is private.
func TestHeadlessScreenStreams(t *testing.T) {
	if os.Getenv("WAYLE_CAPTURE_HEADLESS") == "" {
		t.Skip("WAYLE_CAPTURE_HEADLESS is not set")
	}
	c, err := capture.Connect()
	if err != nil {
		t.Fatal(err)
	}
	outs := c.Outputs()
	_ = c.Close()
	if len(outs) == 0 || outs[0].Name == "" {
		t.Fatalf("outputs = %v", outs)
	}
	pwtest.Start(t)
	for _, target := range []captureTarget{
		{kind: targetOutput, name: outs[0].Name},
		{kind: targetRegion, name: outs[0].Name, x: 10, y: 20, width: 200, height: 100},
	} {
		s, err := startScreenStream(target, true, defaultFPS)
		if err != nil {
			t.Fatalf("%s: %v", target.payload(), err)
		}
		w, h := s.Size()
		if s.NodeID() == 0 || w <= 0 || h <= 0 {
			t.Errorf("%s: node %d size %dx%d", target.payload(), s.NodeID(), w, h)
		}
		if target.kind == targetRegion && (w < 200 || h < 100) {
			t.Errorf("region streams %dx%d", w, h)
		}
		s.Close()
	}
	if _, err := startScreenStream(captureTarget{kind: targetOutput, name: "NOPE-1"}, false, defaultFPS); err == nil {
		t.Error("a missing output streamed")
	}
	if _, err := startScreenStream(captureTarget{kind: targetWindow, name: "no-such-window"}, false, defaultFPS); err == nil {
		t.Error("a missing window streamed")
	}
}

// TestHeadlessGPUStream takes the zero-copy path against a private
// compositor that renders on the GPU (WLR_RENDERER=gles2 on a render
// node); WAYLE_CAPTURE_DMABUF=1 asks for it, with the same rule as
// above: never a developer's screen.
func TestHeadlessGPUStream(t *testing.T) {
	if os.Getenv("WAYLE_CAPTURE_DMABUF") == "" {
		t.Skip("WAYLE_CAPTURE_DMABUF is not set")
	}
	c, err := capture.Connect()
	if err != nil {
		t.Fatal(err)
	}
	outs := c.Outputs()
	_ = c.Close()
	if len(outs) == 0 {
		t.Fatal("no outputs")
	}
	pwtest.Start(t)
	s, err := startGPUStream(outs[0].Name, false, 30)
	if err != nil {
		t.Fatalf("the zero-copy path declined: %v", err)
	}
	defer s.Close()
	if w, h := s.Size(); w <= 0 || h <= 0 || s.NodeID() == 0 {
		t.Errorf("stream %dx%d node %d", w, h, s.NodeID())
	}
}
