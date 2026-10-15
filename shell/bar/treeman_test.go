package bar

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/treeman"
)

// fakeTreeman is a scripted treeman.Source, safe across a follow
// goroutine and the test.
type fakeTreeman struct {
	mu        sync.Mutex
	status    *treeman.Status
	ticks     chan struct{}
	actionErr error
	actions   []string
}

func (f *fakeTreeman) Read(context.Context) (*treeman.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *fakeTreeman) setStatus(s *treeman.Status) {
	f.mu.Lock()
	f.status = s
	f.mu.Unlock()
}

// Subscribe hands out ticks, or a closed channel when none is scripted.
func (f *fakeTreeman) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	if f.ticks != nil {
		return f.ticks, func() {}, nil
	}
	ticks := make(chan struct{}, 4)
	close(ticks)
	return ticks, func() {}, nil
}

func (f *fakeTreeman) RunAction(_ context.Context, a treeman.Action, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, strings.Join(a.Args(path), " "))
	return f.actionErr
}

func (f *fakeTreeman) ran() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.actions)
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
	if got := c.Treeman.IconFailed; got != "tb-x-symbolic" {
		t.Errorf("icon-failed = %q", got)
	}
	if got := c.Treeman.IconPreparing; got != "tb-loader-2-symbolic" {
		t.Errorf("icon-preparing default changed: %q", got)
	}

	if err := osWrite(path, "[modules.treeman]\nformat = \"\"\n"); err != nil {
		t.Fatal(err)
	}
	// The schema puts no constraint on the format string.
	if _, err := config.LoadFile(path); err != nil {
		t.Errorf("empty format: accepted by the schema, got %v", err)
	}
}

// The watcher's update: the icon and severity class follow the worst
// bucket, hide-if-empty collapses the module root on an empty daemon
// and only then, and a missing treeman reads "--".
func TestTreemanModuleFollowsBuckets(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	src := &fakeTreeman{status: &treeman.Status{Total: 1, Stable: 1}}
	ctx.Treeman = src
	module, err := Create("treeman", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm := module.(*treemanModule)
	chrome := widget.NewBox(widget.Row, 0, 0)
	tm.setChrome(chrome)
	label := findLabel(module.Root())
	if got := label.Text(); got != "1" || tm.icon.Name() != cfg.Treeman.IconName || !chrome.Visible() {
		t.Fatalf("resting: label %q icon %q visible %v", got, tm.icon.Name(), chrome.Visible())
	}
	for _, tc := range []struct {
		status      treeman.Status
		icon, class string
	}{
		{treeman.Status{Total: 2, Stable: 1, Up: 1}, cfg.Treeman.IconPreparing, "preparing"},
		{treeman.Status{Total: 2, Up: 1, Down: 1}, cfg.Treeman.IconTearingDown, "tearing-down"},
		{treeman.Status{Total: 2, Stable: 1, Failed: 1}, cfg.Treeman.IconFailed, "failed"},
	} {
		src.status = &tc.status
		if err := tm.refresh(); err != nil {
			t.Fatal(err)
		}
		if tm.icon.Name() != tc.icon || !widget.HasClass(chrome, tc.class) {
			t.Errorf("%+v: icon %q classes %v, want %q and .%s", tc.status, tm.icon.Name(), chrome.Classes(), tc.icon, tc.class)
		}
		for _, other := range []string{"preparing", "tearing-down", "failed"} {
			if other != tc.class && widget.HasClass(chrome, other) {
				t.Errorf("%+v kept .%s", tc.status, other)
			}
		}
	}

	src.status = &treeman.Status{}
	if err := tm.refresh(); err != nil {
		t.Fatal(err)
	}
	if !chrome.Visible() || widget.HasClass(chrome, "failed") {
		t.Errorf("empty without hide-if-empty: visible %v classes %v", chrome.Visible(), chrome.Classes())
	}
	cfg.Treeman.HideIfEmpty = true
	if err := tm.refresh(); err != nil {
		t.Fatal(err)
	}
	if chrome.Visible() {
		t.Error("hide-if-empty left the empty module shown")
	}

	cfg.Treeman.HideIfEmpty = false
	src.status = nil
	if err := tm.refresh(); err != nil {
		t.Fatalf("missing binary refresh: %v", err)
	}
	if got := label.Text(); got != "--" || tm.icon.Name() != cfg.Treeman.IconName {
		t.Errorf("unavailable: label %q icon %q, want -- and the resting icon", got, tm.icon.Name())
	}
}
