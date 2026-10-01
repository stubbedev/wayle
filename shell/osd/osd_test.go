package osd

import (
	"testing"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/widgetipc"
)

func testFont(t *testing.T) render.Font {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func TestAnchors(t *testing.T) {
	// Corners anchor two edges, edges three.
	for _, tc := range []struct {
		position config.OsdPosition
		ok       bool
	}{
		{config.OsdTopLeft, true},
		{config.OsdTop, true},
		{config.OsdTopRight, true},
		{config.OsdRight, true},
		{config.OsdBottomRight, true},
		{config.OsdBottom, true},
		{config.OsdBottomLeft, true},
		{config.OsdLeft, true},
		{"middle", false},
		{"", false},
	} {
		_, ok := anchors(tc.position)
		if ok != tc.ok {
			t.Errorf("anchors(%q) ok = %v, want %v", tc.position, ok, tc.ok)
		}
	}
}

func TestMarginsFor(t *testing.T) {
	cfg := config.DefaultsOsd()
	cfg.Margin = config.Px(16)
	// The bottom position insets the bottom edge only.
	m := marginsFor(cfg)
	if m.Bottom != 16 || m.Top != 0 || m.Left != 0 || m.Right != 0 {
		t.Errorf("bottom margins = %+v", m)
	}
	// A corner insets both of its edges.
	cfg.Position = config.OsdTopRight
	m = marginsFor(cfg)
	if m.Top != 16 || m.Right != 16 || m.Bottom != 0 || m.Left != 0 {
		t.Errorf("top-right margins = %+v", m)
	}
	// A side insets its edge; the ends of the other axis stay free so
	// the centered window can shrink to its fixed size.
	cfg.Position = config.OsdLeft
	m = marginsFor(cfg)
	if m.Left != 16 || m.Top != 0 || m.Bottom != 0 {
		t.Errorf("left margins = %+v", m)
	}
}

func TestClamp01(t *testing.T) {
	for _, tc := range []struct {
		in, want float64
	}{{0, 0}, {0.5, 0.5}, {1, 1}, {-1, 0}, {2, 1}} {
		if got := clamp01(tc.in); got != tc.want {
			t.Errorf("clamp01(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSetEvent(t *testing.T) {
	// setEvent drives the face widgets; without a window the widget
	// half still runs (headless construction).
	font := testFont(t)
	f := &face{
		icon:   widget.NewThemeIcon("", 24),
		label:  widget.NewLabel(font, 14, "", 0),
		slider: widget.NewSlider(0, 1, 0.01, 0),
		value:  widget.NewLabel(font, 14, "", 0),
	}
	f.setEvent(Event{Kind: "volume", Icon: "ld-volume-2-symbolic", Label: "Output", Value: 40})
	if got := f.value.Text(); got != "40%" {
		t.Errorf("value = %q, want 40%%", got)
	}
	if got := f.slider.Value(); got != 0.4 {
		t.Errorf("slider = %v, want 0.4", got)
	}
	f.setEvent(Event{Kind: "volume", Icon: "ld-volume-x-symbolic", Label: "Output", Value: 0, Muted: true})
	if got := f.value.Text(); got != "Muted" {
		t.Errorf("muted value = %q, want Muted", got)
	}
}

func TestDisabledOsdNeverShows(t *testing.T) {
	cfg := config.DefaultsOsd()
	cfg.Enabled = false
	o := New(nil, cfg, config.GeneralConfig{}, config.DefaultsAnimations(), testFont(t), nil)
	o.Show(Event{Kind: "volume"})
	if got := o.Current(); got.Kind != "" {
		t.Errorf("disabled OSD recorded %q", got.Kind)
	}
}

func TestApplyToast(t *testing.T) {
	cfg := config.DefaultsOsd()
	cfg.Presets = []config.ToastPreset{{ID: "screenshot", Label: new("Captured"), Icon: new("ld-camera-symbolic")}}
	o := New(nil, cfg, config.GeneralConfig{}, config.DefaultsAnimations(), testFont(t), nil)

	// A plain label toast without a percentage shows no progress bar.
	ev, err := o.applyToast(widgetipc.ToastRequest{Label: new("hello")})
	if err != nil {
		t.Fatalf("applyToast: %v", err)
	}
	if ev.Label != "hello" || ev.Kind != "toast" {
		t.Errorf("event = %+v", ev)
	}
	if ev.Value != -1 {
		t.Errorf("no-percentage toast got value %v", ev.Value)
	}

	// The preset supplies the defaults; explicit fields override.
	ev, err = o.applyToast(widgetipc.ToastRequest{Preset: new("screenshot")})
	if err != nil {
		t.Fatalf("preset toast: %v", err)
	}
	if ev.Label != "Captured" || ev.Icon != "ld-camera-symbolic" {
		t.Errorf("preset event = %+v", ev)
	}
	ev, err = o.applyToast(widgetipc.ToastRequest{Preset: new("screenshot"), Label: new("custom")})
	if err != nil {
		t.Fatalf("override toast: %v", err)
	}
	if ev.Label != "custom" {
		t.Errorf("override label = %q", ev.Label)
	}

	// Neither label nor preset is an error; an unknown preset too.
	if _, err := o.applyToast(widgetipc.ToastRequest{}); err == nil {
		t.Error("no label: want an error")
	}
	if _, err := o.applyToast(widgetipc.ToastRequest{Preset: new("nope")}); err == nil {
		t.Error("unknown preset: want an error")
	}
	// A percentage clamps into the slider range.
	pct := 140.0
	ev, _ = o.applyToast(widgetipc.ToastRequest{Label: new("x"), Percentage: &pct})
	if ev.Value != 100 {
		t.Errorf("clamped value = %v", ev.Value)
	}
}

func TestOsdLayerFollowsConfigAndTearingMode(t *testing.T) {
	cfg := config.DefaultsOsd()
	o := New(nil, cfg, config.GeneralConfig{}, config.DefaultsAnimations(), testFont(t), nil)
	if o.layer() != app.LayerOverlay {
		t.Errorf("default layer = %d, want overlay", o.layer())
	}
	cfg.Layer = config.LayerBottom
	o.SetConfig(cfg, config.GeneralConfig{}, config.DefaultsAnimations())
	if o.layer() != app.LayerBottom {
		t.Errorf("osd.layer = bottom gave %d", o.layer())
	}
	cfg.Layer = config.LayerOverlay
	o.SetConfig(cfg, config.GeneralConfig{TearingMode: true}, config.DefaultsAnimations())
	if o.layer() != app.LayerTop {
		t.Errorf("overlay under tearing mode = %d, want top", o.layer())
	}
}

func TestGenieEdgeFollowsPosition(t *testing.T) {
	for pos, want := range map[config.OsdPosition]widget.Edge{
		config.OsdTop: widget.EdgeTop, config.OsdTopLeft: widget.EdgeTop, config.OsdTopRight: widget.EdgeTop,
		config.OsdBottom: widget.EdgeBottom, config.OsdBottomLeft: widget.EdgeBottom, config.OsdBottomRight: widget.EdgeBottom,
		config.OsdLeft: widget.EdgeLeft, config.OsdRight: widget.EdgeRight,
	} {
		if got := genieEdge(pos); got != want {
			t.Errorf("%s: %v, want %v", pos, got, want)
		}
	}
}

// TestDismissPlaysTheExit pins begin_dismiss/finish_hide: a face stays
// up through its exit and closes once it lands; a face replaced
// meanwhile is not closed by the old exit.
func TestDismissPlaysTheExit(t *testing.T) {
	o := New(nil, config.DefaultsOsd(), config.GeneralConfig{}, config.DefaultsAnimations(), testFont(t), nil)
	f := &face{rev: widget.NewRevealer(newPlate(widget.NewBox(widget.Row, 0, 0), render.RGB(1, 2, 3)))}
	f.rev.SetRevealed(true)
	f.rev.Finish()
	o.faces["DP-1"] = f
	o.beginDismiss()
	if o.faces["DP-1"] != f || f.rev.Revealed() {
		t.Fatal("dismiss did not start the exit with the face kept up")
	}
	f.rev.Finish()
	if _, ok := o.faces["DP-1"]; ok {
		t.Error("the face outlived its exit")
	}

	g := &face{rev: widget.NewRevealer(widget.NewBox(widget.Row, 0, 0))}
	g.rev.SetRevealed(true)
	g.rev.Finish()
	o.faces["DP-1"] = g
	o.beginDismiss()
	replacement := &face{rev: widget.NewRevealer(widget.NewBox(widget.Row, 0, 0))}
	o.faces["DP-1"] = replacement
	g.rev.Finish()
	if o.faces["DP-1"] != replacement {
		t.Error("an old exit closed the face that replaced it")
	}
}

func TestPlatePaintsUnderItsContent(t *testing.T) {
	p := newPlate(widget.NewBox(widget.Row, 0, 0), render.RGB(10, 20, 30))
	p.Measure(widget.Constraints{Max: widget.Size{W: 4, H: 4}})
	p.Arrange(render.Rect{W: 4, H: 4})
	data := make([]byte, render.Stride(4)*4)
	p.Paint(render.New(data, render.Stride(4), 4, 4))
	if data[2] != 10 || data[1] != 20 || data[0] != 30 {
		t.Errorf("plate pixel = %v, want the background", data[:4])
	}
}
