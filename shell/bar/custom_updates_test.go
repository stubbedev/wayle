package bar

import (
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/widgetipc"
	"github.com/stubbedev/wayle/shell/osd"
)

func TestCustomUpdatesDispatch(t *testing.T) {
	cfg := config.Defaults()
	// A custom module with no command: it exists purely for pushes.
	def := config.CustomModuleDefinition{Id: "pushed", LabelShow: true, Format: "{{ text }}"}
	cfg.Custom = append(cfg.Custom, def)
	ctx := newTestContext(t, cfg)
	ctx.CustomUpdates = newCustomUpdates()
	upd := ctx.CustomUpdates

	// Build the module the way newCustom would, minus the loop.
	m := &customModule{ctx: ctx, def: def}
	m.label = newTestLabel(t, ctx)
	upd.register(m)

	upd.dispatch("pushed", `{"text":"pushed-payload"}`)
	if got := m.label.Text(); got != "pushed-payload" {
		t.Errorf("label = %q, want pushed-payload", got)
	}
	// Plain output replaces the whole label through {{ output }}.
	def2 := def
	def2.Id = "raw"
	def2.Format = "{{ output }}"
	m2 := &customModule{ctx: ctx, def: def2}
	m2.label = newTestLabel(t, ctx)
	upd.register(m2)
	upd.dispatch("raw", "plain text")
	if got := m2.label.Text(); got != "plain text" {
		t.Errorf("label = %q", got)
	}
	// An unknown id is a quiet no-op.
	upd.dispatch("ghost", "ignored")
	// Stop unregisters.
	m.Stop()
	upd.dispatch("pushed", `{"text":"after-stop"}`)
	if got := m.label.Text(); got != "pushed-payload" {
		t.Errorf("a stopped module still received pushes: %q", got)
	}
}

func TestToastApplyViaWidgetSocket(t *testing.T) {
	// applyToast rides the OSD tests; here the wiring shape: a ToastRequest
	// with neither label nor preset errors, which RunWith logs.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.DefaultsOsd()
	o := osd.New(nil, cfg, config.GeneralConfig{}, testFont(t), nil)
	if err := o.ShowToast(widgetipc.ToastRequest{}); err == nil {
		t.Fatal("empty toast: want an error")
	}
}

// newTestLabel builds a label for hand-rolled module fixtures.
func newTestLabel(t *testing.T, ctx ModuleContext) *widget.Label {
	t.Helper()
	return widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
}
