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
