package hyprsunset

import (
	"testing"
	"time"
)

func persistNow() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }

// The persist.rs test cases.
func TestParseOverride(t *testing.T) {
	if rec, ok := parseOverride("day on 2026-09-05T11:59:00Z", persistNow()); !ok || rec.Phase != PhaseDay || !rec.Enabled {
		t.Errorf("fresh record = %+v, %v", rec, ok)
	}
	if rec, ok := parseOverride("none off 2026-09-05T11:00:00Z", persistNow()); !ok || rec.Phase != 0 || rec.Enabled {
		t.Errorf("phase-less off record = %+v, %v", rec, ok)
	}
	if _, ok := parseOverride("night on 2026-09-04T12:00:00Z", persistNow()); ok {
		t.Error("a stale record was honoured")
	}
	for _, bad := range []string{"night on", "dusk on 2026-09-05T11:59:00Z", "", "day on yesterday"} {
		if _, ok := parseOverride(bad, persistNow()); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestOverrideRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LoadOverride(dir, persistNow()); ok {
		t.Fatal("an empty dir has a record")
	}
	SaveOverride(dir, Override{Phase: PhaseNight, Enabled: true}, persistNow())
	rec, ok := LoadOverride(dir, persistNow().Add(time.Hour))
	if !ok || rec.Phase != PhaseNight || !rec.Enabled {
		t.Fatalf("round trip = %+v, %v", rec, ok)
	}
	if _, ok := LoadOverride(dir, persistNow().Add(13*time.Hour)); ok {
		t.Error("the record outlived its 12h age limit")
	}
	ClearOverride(dir)
	if _, ok := LoadOverride(dir, persistNow()); ok {
		t.Error("the record survived ClearOverride")
	}
}
