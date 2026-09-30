package hyprsunset

import (
	"testing"
	"time"
)

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

// The solar.rs test cases, verbatim.
func TestPhaseAtMatchesTheRustCases(t *testing.T) {
	for _, tc := range []struct {
		name     string
		now      time.Time
		lat, lng float64
		want     Phase
	}{
		{"copenhagen summer noon is day", utc(2024, 6, 21, 12, 0), 55.6, 12.5, PhaseDay},
		{"copenhagen summer predawn is night", utc(2024, 6, 21, 1, 0), 55.6, 12.5, PhaseNight},
		{"copenhagen winter evening is night", utc(2024, 12, 21, 17, 0), 55.6, 12.5, PhaseNight},
		{"copenhagen winter midday is day", utc(2024, 12, 21, 11, 0), 55.6, 12.5, PhaseDay},
		{"arctic midwinter is night all day", utc(2024, 12, 21, 12, 0), 78.2, 15.6, PhaseNight},
		{"arctic midsummer is day all night", utc(2024, 6, 21, 0, 0), 78.2, 15.6, PhaseDay},
	} {
		if got := PhaseAt(tc.now, tc.lat, tc.lng); got != tc.want {
			t.Errorf("%s: phase = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Almanac sunrise/sunset times (timeanddate.com, UTC) the NOAA
// equation lands within a few minutes of.
func TestSunTimesAgainstKnownValues(t *testing.T) {
	// The simplified equation (one day number per UTC date) drifts a few
	// minutes at longitudes far from Greenwich.
	const tolerance = 8 * time.Minute
	for _, tc := range []struct {
		name           string
		day            time.Time
		lat, lng       float64
		rise, set      time.Time
		polarDayWant   bool
		polarNightWant bool
	}{
		{name: "copenhagen midsummer", day: utc(2024, 6, 21, 12, 0), lat: 55.6761, lng: 12.5683, rise: utc(2024, 6, 21, 2, 25), set: utc(2024, 6, 21, 19, 57)},
		{name: "london midwinter", day: utc(2024, 12, 21, 12, 0), lat: 51.5074, lng: -0.1278, rise: utc(2024, 12, 21, 8, 4), set: utc(2024, 12, 21, 15, 54)},
		{name: "new york equinox", day: utc(2024, 3, 20, 12, 0), lat: 40.7128, lng: -74.0060, rise: utc(2024, 3, 20, 11, 3), set: utc(2024, 3, 20, 23, 14)},
		{name: "sydney midwinter", day: utc(2024, 6, 21, 12, 0), lat: -33.8688, lng: 151.2093, rise: utc(2024, 6, 20, 20, 59), set: utc(2024, 6, 21, 6, 54)},
		{name: "svalbard polar night", day: utc(2024, 12, 21, 12, 0), lat: 78.2, lng: 15.6, polarNightWant: true},
		{name: "svalbard midnight sun", day: utc(2024, 6, 21, 12, 0), lat: 78.2, lng: 15.6, polarDayWant: true},
	} {
		st := sunTimesFor(tc.day, tc.lat, tc.lng)
		if st.polarDay != tc.polarDayWant || st.polarNight != tc.polarNightWant {
			t.Errorf("%s: polar day/night = %v/%v, want %v/%v", tc.name, st.polarDay, st.polarNight, tc.polarDayWant, tc.polarNightWant)
			continue
		}
		if tc.polarDayWant || tc.polarNightWant {
			continue
		}
		if d := st.sunrise.Sub(tc.rise).Abs(); d > tolerance {
			t.Errorf("%s: sunrise %s, want %s (off by %v)", tc.name, st.sunrise.Format(time.TimeOnly), tc.rise.Format(time.TimeOnly), d)
		}
		if d := st.sunset.Sub(tc.set).Abs(); d > tolerance {
			t.Errorf("%s: sunset %s, want %s (off by %v)", tc.name, st.sunset.Format(time.TimeOnly), tc.set.Format(time.TimeOnly), d)
		}
	}
}

// The phase flips exactly at the computed boundaries.
func TestPhaseFlipsAtTheBoundaries(t *testing.T) {
	day := utc(2024, 3, 20, 12, 0)
	st := sunTimesFor(day, 51.5074, -0.1278)
	if got := PhaseAt(st.sunrise.Add(-time.Second), 51.5074, -0.1278); got != PhaseNight {
		t.Errorf("a second before sunrise = %v, want night", got)
	}
	if got := PhaseAt(st.sunrise, 51.5074, -0.1278); got != PhaseDay {
		t.Errorf("at sunrise = %v, want day (inclusive)", got)
	}
	if got := PhaseAt(st.sunset, 51.5074, -0.1278); got != PhaseNight {
		t.Errorf("at sunset = %v, want night (exclusive)", got)
	}
}

func TestJulianConversion(t *testing.T) {
	// J2000.0 is 2000-01-01 12:00 TT; the formula treats it as UTC.
	got, ok := julianToUTC(2_451_545.0)
	if !ok || !got.Equal(utc(2000, 1, 1, 12, 0)) {
		t.Errorf("J2000 = %v, %v", got, ok)
	}
	if _, ok := julianToUTC(nan()); ok {
		t.Error("NaN converted to a time")
	}
}

func nan() float64 {
	zero := 0.0
	return zero / zero
}
