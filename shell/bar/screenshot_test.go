package bar

import (
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

func TestScreenshotBuiltin(t *testing.T) {
	for _, tc := range []struct {
		verb, mode, target string
	}{
		{"screenshot", "region", ""},
		{"screenshot region", "region", ""},
		{"screenshot output DP-1", "output", "DP-1"},
		{"screenshot  window ", "window", ""},
	} {
		mode, target, ok := screenshotBuiltin(tc.verb)
		if !ok || mode != tc.mode || target != tc.target {
			t.Errorf("%q = %q %q %v", tc.verb, mode, target, ok)
		}
	}
	for _, verb := range []string{"", "screenshots region", "recorder toggle"} {
		if _, _, ok := screenshotBuiltin(verb); ok {
			t.Errorf("%q read as a screenshot builtin", verb)
		}
	}
}

func TestScreenshotBindingRunsInProcess(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	var got []string
	ctx.Screenshot = func(mode, target string) { got = append(got, mode+"|"+target) }
	runClickAction(ctx, ctx.Config.Screenshot.Clicks().RightClick)
	runShellBuiltin(ctx, "wayle screenshot output HDMI-A-1")
	if len(got) != 2 || got[0] != "output|" || got[1] != "output|HDMI-A-1" {
		t.Fatalf("triggered %v", got)
	}
	// Other builtins never reach the screenshot host.
	runShellBuiltin(ctx, "wayle audio output-mute")
	if len(got) != 2 {
		t.Fatalf("a foreign builtin triggered a capture: %v", got)
	}
}

func TestScreenshotModule(t *testing.T) {
	cfg := config.Defaults()
	m, err := newScreenshot(newTestContext(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Root().(*widget.Icon); !ok {
		t.Fatalf("default root %T, want the lone icon", m.Root())
	}
	cfg.Screenshot.Label, cfg.Screenshot.LabelShow, cfg.Screenshot.LabelMaxLength = "Capture", true, 4
	m, err = newScreenshot(newTestContext(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	btn, ok := m.Root().(*barButton)
	if !ok || btn.icon == nil || btn.label == nil {
		t.Fatalf("labelled root %T, want the icon + label bar button", m.Root())
	}
	if got := btn.label.Text(); got != "Cap…" {
		t.Fatalf("label %q, want Cap…", got)
	}
	cfg.Screenshot.IconShow, cfg.Screenshot.LabelShow = false, false
	if _, err := newScreenshot(newTestContext(t, cfg)); err == nil {
		t.Fatal("a module with nothing to show was built")
	}
	cfg.Screenshot.LabelShow = true
	m, err = newScreenshot(newTestContext(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if btn, ok := m.Root().(*barButton); !ok || btn.icon != nil || btn.label == nil {
		t.Fatalf("label-only root %T, want a bar button with only a label", m.Root())
	}
}
