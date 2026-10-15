package bar

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// stubModule is a module that is nothing but its root.
type stubModule struct{ root *widget.Box }

func (m stubModule) Root() widget.Widget { return m.root }

// An unavailable module is left out of its row and the rest of the
// section still builds: one missing daemon never takes the bar down.
func TestCreateAllSkipsModuleThatFailsToBuild(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	ok := stubModule{root: widget.NewBox(widget.Row, 0, 0)}
	factories["test-ok"] = func(ModuleContext) (Module, error) { return ok, nil }
	factories["test-missing"] = func(ModuleContext) (Module, error) {
		return nil, errors.New("upower: The name is not activatable")
	}
	defer delete(factories, "test-ok")
	defer delete(factories, "test-missing")

	row := CreateAll([]config.BarItem{{Module: "test-missing"}, {Module: "test-ok"}}, ctx)

	items := row.Children()
	if len(items) != 2 {
		t.Fatalf("section items = %d, want 2 (one bar-item per layout item)", len(items))
	}
	if got := len(items[0].(*widget.Box).Children()); got != 0 {
		t.Fatalf("failed module's bar-item children = %d, want 0 (the module is left out)", got)
	}
	if got := len(items[1].(*widget.Box).Children()); got != 1 {
		t.Fatalf("working module's bar-item children = %d, want 1", got)
	}
}

// An unknown module name is logged and skipped the same way, instead
// of failing the whole bar.
func TestCreateAllSkipsUnknownModule(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	row := CreateAll([]config.BarItem{{Module: "no-such-module"}}, ctx)
	if got := len(row.Children()[0].(*widget.Box).Children()); got != 0 {
		t.Fatalf("unknown module's bar-item children = %d, want 0", got)
	}
}

// updates is in the schema without a module in either shell: its slot
// is skipped as "not implemented" (the Rust factory's warning) and the
// rest of the section builds.
func TestUnimplementedSchemaModuleIsSkipped(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	if _, err := Create("updates", ctx); err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("updates: %v, want a not-implemented error", err)
	}
	items := CreateAll([]config.BarItem{{Module: "updates"}, {Module: "clock"}}, ctx).Children()
	if len(items[0].(*widget.Box).Children()) != 0 || len(items[1].(*widget.Box).Children()) != 1 {
		t.Error("want the updates item empty and the clock built")
	}
}
