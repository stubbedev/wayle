// Package hyprsunset is the hyprsunset module's service side: the
// blue-light filter process and its control socket, the sunrise/sunset
// schedule (NOAA sunrise equation, no network), the GeoClue location
// lookup, and the manual-override record that survives restarts
// (crates/wayle-bar-workspaces/src/modules/hyprsunset).
package hyprsunset

import (
	"math"
	"time"
)

// Phase is whether the sun is up at a place and time.
type Phase uint8

// Solar phases.
const (
	PhaseDay Phase = iota + 1
	PhaseNight
)

// String is the persist-record spelling.
func (p Phase) String() string {
	switch p {
	case PhaseDay:
		return "day"
	case PhaseNight:
		return "night"
	}
	return "none"
}

// sunTimes is one date's outcome: a sunrise/sunset pair, or the sun
// never setting or never rising.
type sunTimes struct {
	sunrise, sunset time.Time
	polarDay        bool
	polarNight      bool
}

// PhaseAt is solar.rs phase_at: day between sunrise and sunset of now's
// UTC date, night otherwise; polar day is day and polar night is night.
// Latitude and longitude are decimal degrees, north and east positive.
func PhaseAt(now time.Time, latitude, longitude float64) Phase {
	st := sunTimesFor(now, latitude, longitude)
	switch {
	case st.polarDay:
		return PhaseDay
	case st.polarNight:
		return PhaseNight
	case !now.Before(st.sunrise) && now.Before(st.sunset):
		return PhaseDay
	}
	return PhaseNight
}

// unixEpochDaysFromCE is chrono's num_days_from_ce of 1970-01-01.
const unixEpochDaysFromCE = 719163

// remEuclid is f64::rem_euclid: the non-negative remainder.
func remEuclid(x, m float64) float64 {
	r := math.Mod(x, m)
	if r < 0 {
		r += m
	}
	return r
}

func radians(deg float64) float64 { return deg * math.Pi / 180 }

func degrees(rad float64) float64 { return rad * 180 / math.Pi }

// sunTimesFor is solar.rs sun_times, step for step.
func sunTimesFor(now time.Time, latitude, longitude float64) sunTimes {
	utc := now.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	daysFromCE := float64(midnight.Unix()/86400 + unixEpochDaysFromCE)
	// Julian day number (integer, at noon) for the date of now.
	jdn := daysFromCE + 1_721_425.0

	// Days since J2000, with leap-second fudge. lW is longitude WEST.
	lW := -longitude
	n := math.Round(jdn - 2_451_545.0 + 0.0008)

	// Mean solar time: solar noon comes later the further west, so the
	// west longitude ADDS. solar.rs subtracts it here, which mirrors
	// every schedule about Greenwich (Copenhagen's sunrise lands 1h40m
	// late, New York's ten hours off); the known-value tests pin the
	// corrected sign.
	jStar := n + lW/360.0

	// Solar mean anomaly (degrees).
	m := remEuclid(357.5291+0.985_600_28*jStar, 360.0)
	mRad := radians(m)

	// Equation of the center (degrees).
	c := 1.9148*math.Sin(mRad) + 0.0200*math.Sin(2.0*mRad) + 0.0003*math.Sin(3.0*mRad)

	// Ecliptic longitude (degrees).
	lambda := remEuclid(m+c+180.0+102.9372, 360.0)
	lambdaRad := radians(lambda)

	// Solar transit (Julian date of solar noon).
	jTransit := 2_451_545.0 + jStar + 0.0053*math.Sin(mRad) - 0.0069*math.Sin(2.0*lambdaRad)

	// Sun declination.
	sinDecl := math.Sin(lambdaRad) * math.Sin(radians(23.4397))
	decl := math.Asin(sinDecl)

	// Hour angle (with -0.833 degrees for refraction + solar disc radius).
	latRad := radians(latitude)
	cosOmega := (math.Sin(radians(-0.833)) - math.Sin(latRad)*math.Sin(decl)) /
		(math.Cos(latRad) * math.Cos(decl))

	if cosOmega < -1.0 {
		return sunTimes{polarDay: true} // sun never sets
	}
	if cosOmega > 1.0 {
		return sunTimes{polarNight: true} // sun never rises
	}

	omega := degrees(math.Acos(cosOmega))
	jRise := jTransit - omega/360.0
	jSet := jTransit + omega/360.0

	rise, okRise := julianToUTC(jRise)
	set, okSet := julianToUTC(jSet)
	if !okRise || !okSet {
		// Numerically degenerate: the safer "day" keeps the filter off.
		return sunTimes{polarDay: true}
	}
	return sunTimes{sunrise: rise, sunset: set}
}

// julianToUTC converts a Julian Date to a UTC instant, rounded to the
// second; false when it is not a finite time.
func julianToUTC(jd float64) (time.Time, bool) {
	secs := (jd - 2_440_587.5) * 86_400.0
	if math.IsNaN(secs) || math.IsInf(secs, 0) {
		return time.Time{}, false
	}
	return time.Unix(int64(math.Round(secs)), 0).UTC(), true
}
