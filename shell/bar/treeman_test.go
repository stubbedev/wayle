package bar

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/treeman"
)

// fakeTreeman is a scripted treeman.Source.
type fakeTreeman struct {
	status *treeman.Status
}

func (f *fakeTreeman) Read(context.Context) (*treeman.Status, error) { return f.status, nil }

func (f *fakeTreeman) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	ticks := make(chan struct{}, 4)
	close(ticks)
	return ticks, func() {}, nil
}

func TestTreemanLabel(t *testing.T) {
	status := &treeman.Status{Total: 5, Stable: 2, Up: 1, Down: 1, Failed: 1}
	if got := treemanLabel("{{ total }}: {{ stable }} {{ up }} {{ down }} {{ failed }}", status); got != "5: 2 1 1 1" {
		t.Errorf("= %q", got)
	}
	if got := treemanLabel("{{ total }}", &treeman.Status{}); got != "0" {
		t.Errorf("= %q", got)
	}
}

func TestLoadFileAppliesTreeman(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.treeman]\nformat = \"[{{ total }}]\"\nhide-if-empty = true\nicon-failed = \"tb-x-symbolic\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Treeman.Format != "[{{ total }}]" || !c.Treeman.HideIfEmpty {
		t.Errorf("config = %+v", c.Treeman)
	}
	if got := c.Treeman.Icons[config.TreemanBucketFailed].Name; got != "tb-x-symbolic" {
		t.Errorf("icon-failed = %q", got)
	}
	if got := c.Treeman.Icons[config.TreemanBucketUp].Name; got != "tb-loader-2-symbolic" {
		t.Errorf("icon-preparing default changed: %q", got)
	}

	if err := osWrite(path, "[modules.treeman]\nformat = \"\"\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err == nil {
		t.Error("empty format: want a load error")
	}
}

func TestTreemanModuleFollowsBuckets(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	src := &fakeTreeman{status: &treeman.Status{Total: 1, Stable: 1}}
	ctx.Treeman = src
	// The module needs the loop for its subscription; construct through
	// the factory with a headless context and drive refresh by hand.
	module, err := Create("treeman", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm := module.(*treemanModule)
	label := findLabel(module.Root())
	if got := label.Text(); got != "1" {
		t.Fatalf("label = %q, want 1", got)
	}

	// A failure in the buckets flips the icon.
	icon := tm.icon.(*widget.Icon)
	src.status = &treeman.Status{Total: 2, Stable: 1, Failed: 1}
	if err := tm.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := icon.Name(); got != cfg.Treeman.Icons[config.TreemanBucketFailed].Name {
		t.Errorf("failed icon = %q", got)
	}

	// hide-if-empty collapses the label on an empty daemon.
	cfg.Treeman.HideIfEmpty = true
	src.status = &treeman.Status{}
	if err := tm.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := label.Text(); got != "" {
		t.Errorf("hidden label = %q", got)
	}

	// A missing treeman reads as unavailable: an empty label, no error.
	src.status = nil
	if err := tm.refresh(); err != nil {
		t.Fatalf("missing binary refresh: %v", err)
	}
	if got := label.Text(); got != "" {
		t.Errorf("unavailable label = %q", got)
	}
}
