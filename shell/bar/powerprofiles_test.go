package bar

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/powerprofiles"
)

// fakePPSource is the scripted power-profiles source, safe across a
// follow goroutine and the test. SetActive records the profile and, as
// the daemon does, makes it active.
type fakePPSource struct {
	mu    sync.Mutex
	snap  powerprofiles.Snapshot
	set   []string
	err   error
	ticks chan struct{}
}

func (f *fakePPSource) Read(context.Context) (powerprofiles.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, f.err
}

func (f *fakePPSource) SetActive(_ context.Context, profile string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.set = append(f.set, profile)
	f.snap.Active = profile
	return nil
}

// sets are the SetActive arguments so far.
func (f *fakePPSource) sets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.set)
}

// Subscribe hands out ticks, or a closed channel when none is scripted.
func (f *fakePPSource) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	if f.ticks != nil {
		return f.ticks, func() {}, nil
	}
	ticks := make(chan struct{})
	close(ticks)
	return ticks, func() {}, nil
}

func TestPowerProfilesLabel(t *testing.T) {
	if got := powerProfilesLabel("{{ profile }}", "balanced"); got != "balanced" {
		t.Errorf("= %q", got)
	}
	if got := powerProfilesLabel("[{{ profile }}]", "performance"); got != "[performance]" {
		t.Errorf("= %q", got)
	}
}

func TestLoadFileAppliesPowerProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.power-profiles]\nformat = \"P: {{ profile }}\"\nlabel-show = true\nicon-performance = \"ld-zap-symbolic\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.PowerProfiles.Format != "P: {{ profile }}" || !c.PowerProfiles.LabelShow {
		t.Errorf("config = %+v", c.PowerProfiles)
	}
	if got := c.PowerProfiles.Icons()[config.ProfilePerformance].Name; got != "ld-zap-symbolic" {
		t.Errorf("performance icon = %q", got)
	}
	// The schema's default left-click is :cycle.
	if c.PowerProfiles.Clicks().LeftClick.String() != ":cycle" {
		t.Errorf("left-click = %q", c.PowerProfiles.Clicks().LeftClick.String())
	}

	if err := osWrite(path, "[modules.power-profiles]\nformat = \"\"\n"); err != nil {
		t.Fatal(err)
	}
	// The schema puts no constraint on the format string.
	if _, err := config.LoadFile(path); err != nil {
		t.Errorf("empty format: accepted by the schema, got %v", err)
	}
}

func TestPowerProfilesModuleCycles(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	src := &fakePPSource{snap: powerprofiles.Snapshot{
		Available: true,
		Active:    powerprofiles.ProfileBalanced,
		Profiles:  []string{powerprofiles.ProfileBalanced, powerprofiles.ProfilePerformance},
	}}
	ctx.PowerProfiles = src

	module, err := Create("power-profiles", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pp, ok := module.(*powerProfilesModule)
	if !ok {
		t.Fatalf("Create = %T", module)
	}
	// The :cycle shell action is the module's own.
	pp.RunAction(config.ParseClickAction(":cycle"))
	if len(src.sets()) != 1 || src.sets()[0] != powerprofiles.ProfilePerformance {
		t.Errorf("set = %v, want [performance]", src.sets())
	}
	// A dropdown action falls through to the shared executor (a no-op
	// log, not a profile write).
	pp.RunAction(config.ParseClickAction("dropdown:power"))
	if len(src.sets()) != 1 {
		t.Errorf("dropdown action reached the daemon: %v", src.sets())
	}
}

func TestNewPowerProfilesRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.PowerProfiles = nil
	if _, err := Create("power-profiles", ctx); err == nil {
		t.Fatal("no daemon: want an error, got a module")
	}
}
