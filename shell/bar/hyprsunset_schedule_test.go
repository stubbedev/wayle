package bar

import (
	"testing"
	"time"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/hyprsunset"
)

// fakeSunset is a scripted sunsetEnv: a fake filter, a settable clock,
// and a temp state dir.
type fakeSunset struct {
	running bool
	starts  int
	stops   int
	clock   time.Time
}

func (f *fakeSunset) env(t *testing.T) sunsetEnv {
	return sunsetEnv{
		start:    func(int, int) error { f.running = true; f.starts++; return nil },
		stop:     func() error { f.running = false; f.stops++; return nil },
		query:    func() (hyprsunset.State, bool) { return hyprsunset.State{}, f.running },
		now:      func() time.Time { return f.clock },
		stateDir: t.TempDir(),
	}
}

// Copenhagen, where 2024-06-21 01:00 UTC is night and 12:00 day.
func sunsetConfig(auto bool) *config.Config {
	cfg := config.Defaults()
	cfg.Hyprsunset.AutoSchedule = auto
	cfg.Hyprsunset.Latitude, cfg.Hyprsunset.Longitude = 55.6, 12.5
	return cfg
}

var (
	sunsetNight = time.Date(2024, 6, 21, 1, 0, 0, 0, time.UTC)
	sunsetDay   = time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
)

func TestHyprsunsetToggleFlipsTheFilterAndIcon(t *testing.T) {
	cfg := sunsetConfig(false)
	fake := &fakeSunset{clock: sunsetDay}
	m := newHyprsunsetWith(newTestContext(t, cfg), fake.env(t))
	if m.enabled || m.icon.Name() != cfg.Hyprsunset.IconOff || m.label.Text() != "Off" {
		t.Fatalf("resting = enabled %v icon %q label %q", m.enabled, m.icon.Name(), m.label.Text())
	}
	m.RunAction(config.ParseClickAction(":toggle"))
	if !fake.running || !m.enabled || m.icon.Name() != "ld-moon-symbolic" || m.label.Text() != "On" {
		t.Fatalf("after toggle = running %v enabled %v icon %q", fake.running, m.enabled, m.icon.Name())
	}
	m.RunAction(config.ParseClickAction(":toggle"))
	if fake.running || m.enabled {
		t.Fatal("the second toggle did not stop the filter")
	}
	// Other actions do not toggle.
	m.RunAction(config.ParseClickAction("dropdown:hyprsunset"))
	if fake.starts != 1 {
		t.Errorf("starts = %d, want only the one toggle", fake.starts)
	}
	// Without the schedule, evaluating drives nothing.
	m.evaluate()
	if fake.running {
		t.Error("evaluate drove the filter with auto-schedule off")
	}
}

func TestHyprsunsetScheduleFollowsTheSun(t *testing.T) {
	fake := &fakeSunset{clock: sunsetNight}
	m := newHyprsunsetWith(newTestContext(t, sunsetConfig(true)), fake.env(t))
	m.evaluate()
	if !fake.running {
		t.Fatal("night: the schedule did not enable the filter")
	}
	fake.clock = sunsetDay
	m.evaluate()
	if fake.running {
		t.Fatal("day: the schedule did not disable the filter")
	}
}

func TestHyprsunsetManualOverrideHoldsUntilTheBoundary(t *testing.T) {
	fake := &fakeSunset{clock: sunsetNight}
	env := fake.env(t)
	m := newHyprsunsetWith(newTestContext(t, sunsetConfig(true)), env)
	m.evaluate() // night: on
	m.toggle()   // the user turns it off at night
	if fake.running {
		t.Fatal("toggle did not stop the filter")
	}
	m.evaluate()
	if fake.running {
		t.Fatal("the schedule overrode the manual toggle within the same phase")
	}
	if rec, ok := hyprsunset.LoadOverride(env.stateDir, fake.clock); !ok || rec.Enabled || rec.Phase != hyprsunset.PhaseNight {
		t.Errorf("record = %+v, %v; want night/off", rec, ok)
	}
	// Crossing into day hands control back and drops the record.
	fake.clock = sunsetDay
	m.evaluate()
	if m.manualOverride {
		t.Error("the override survived the phase change")
	}
	if _, ok := hyprsunset.LoadOverride(env.stateDir, fake.clock); ok {
		t.Error("the record survived the phase change")
	}
	// The next night the schedule drives again.
	fake.clock = sunsetNight.Add(24 * time.Hour)
	m.evaluate()
	if !fake.running {
		t.Error("the schedule did not resume after the boundary")
	}
}

func TestHyprsunsetRestartReplaysTheToggle(t *testing.T) {
	fake := &fakeSunset{clock: sunsetDay}
	env := fake.env(t)
	cfg := sunsetConfig(true)
	hyprsunset.SaveOverride(env.stateDir, hyprsunset.Override{Phase: hyprsunset.PhaseDay, Enabled: true}, sunsetDay.Add(-time.Minute))
	m := newHyprsunsetWith(newTestContext(t, cfg), env)
	if !fake.running || !m.enabled {
		t.Fatal("a remembered manual enable was not replayed")
	}
	// Day would turn it off, but the replayed override holds.
	m.evaluate()
	if !fake.running {
		t.Error("the schedule dropped the replayed override in the same phase")
	}
	// A remembered disable starts nothing.
	fake2 := &fakeSunset{clock: sunsetDay}
	env2 := fake2.env(t)
	hyprsunset.SaveOverride(env2.stateDir, hyprsunset.Override{Enabled: false}, sunsetDay)
	newHyprsunsetWith(newTestContext(t, cfg), env2)
	if fake2.starts != 0 {
		t.Error("a remembered disable started the filter")
	}
}

func TestHyprsunsetStateAndLocation(t *testing.T) {
	cfg := sunsetConfig(true)
	fake := &fakeSunset{clock: sunsetDay}
	m := newHyprsunsetWith(newTestContext(t, cfg), fake.env(t))
	m.applyState(hyprsunset.State{Temp: 4200, Gamma: 90}, true)
	if !m.enabled || m.temp != 4200 {
		t.Errorf("applyState = %v %d", m.enabled, m.temp)
	}
	m.applyState(hyprsunset.State{}, false)
	if m.enabled || m.temp != int(cfg.Hyprsunset.Temperature) {
		t.Errorf("off = %v %d, want the configured temp", m.enabled, m.temp)
	}
	// A GeoClue fix replaces the configured coordinates: at 12:00 UTC
	// it is night in Sydney.
	m.setLocation(hyprsunset.Location{Latitude: -33.87, Longitude: 151.21})
	if !fake.running {
		t.Error("the resolved location did not drive the schedule")
	}
}
