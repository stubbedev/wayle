package weather

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFromWmoCodePinsTheRustEdges(t *testing.T) {
	for _, tc := range []struct {
		code uint8
		want Condition
	}{
		{0, CondClear},
		{1, CondPartlyCloudy},
		{2, CondPartlyCloudy},
		{3, CondOvercast},
		{45, CondFog},
		{48, CondFog},
		{51, CondDrizzle},
		{56, CondSleet},
		{61, CondLightRain},
		{63, CondRain},
		{65, CondHeavyRain},
		{66, CondSleet},
		{71, CondLightSnow},
		{73, CondSnow},
		{75, CondHeavySnow},
		{77, CondSnow},
		{80, CondRain},
		{85, CondSnow},
		{95, CondThunderstorm},
		{99, CondThunderstorm},
		{42, CondUnknown},
	} {
		if got := FromWmoCode(tc.code); got != tc.want {
			t.Errorf("wmo %d = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestCardinalBuckets(t *testing.T) {
	for _, tc := range []struct {
		deg  uint16
		want string
	}{
		{0, "N"},
		{22, "N"},
		{359, "N"},
		{23, "NE"},
		{67, "NE"},
		{68, "E"},
		{112, "E"},
		{113, "SE"},
		{157, "SE"},
		{158, "S"},
		{202, "S"},
		{203, "SW"},
		{247, "SW"},
		{248, "W"},
		{292, "W"},
		{293, "NW"},
		{337, "NW"},
	} {
		if got := WindDirection(tc.deg).Cardinal(); got != tc.want {
			t.Errorf("cardinal(%d) = %q, want %q", tc.deg, got, tc.want)
		}
	}
}

func TestCurrentHourIndexIsTheHourInProgress(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.Local)
	times := []string{"2026-10-01T12:00", "2026-10-01T13:00", "2026-10-01T14:00", "2026-10-01T15:00"}
	if got := currentHourIndex(times, now); got != 2 {
		t.Errorf("index = %d, want 2 (14:00 is in progress)", got)
	}
	// A forecast entirely in the past falls back to 0.
	if got := currentHourIndex(times[:2], now); got != 0 {
		t.Errorf("stale forecast index = %d, want the fallback 0", got)
	}
}

func TestParseLocationCoordinatesOrCity(t *testing.T) {
	if got := ParseLocation(" 52.52 , 13.40 "); !got.Coordinates || got.Lat != 52.52 || got.Lon != 13.40 {
		t.Errorf("coords = %+v", got)
	}
	// A comma without two numbers is a city name, kept whole.
	if got := ParseLocation("Paris, France"); got.Coordinates || got.City != "Paris, France" {
		t.Errorf("city = %+v", got)
	}
}

// openMeteoFixture is a two-hour, one-day Open-Meteo reply.
func openMeteoFixture(hours ...string) string {
	n := len(hours)
	arr := func(v float64) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprint(v)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	quoted := make([]string, n)
	for i, h := range hours {
		quoted[i] = `"` + h + `"`
	}
	return `{"hourly":{"time":[` + strings.Join(quoted, ",") + `],` +
		`"temperature_2m":` + arr(21.5) + `,"relative_humidity_2m":` + arr(60) + `,"apparent_temperature":` + arr(20) +
		`,"precipitation_probability":` + arr(30) + `,"precipitation":` + arr(0.2) + `,"weather_code":` + arr(61) +
		`,"cloud_cover":` + arr(140) + `,"pressure_msl":` + arr(1013) + `,"visibility":` + arr(24000) +
		`,"wind_speed_10m":` + arr(12) + `,"wind_direction_10m":` + arr(370) + `,"wind_gusts_10m":` + arr(-3) +
		`,"dew_point_2m":` + arr(9) + `,"uv_index":` + arr(20) + `,"is_day":` + arr(1) + `},` +
		`"daily":{"time":["2026-10-01"],"weather_code":[3],"temperature_2m_max":[24],"temperature_2m_min":[12],` +
		`"relative_humidity_2m_mean":[55],"sunrise":["2026-10-01T07:12"],"sunset":["2026-10-01T18:45"],"uv_index_max":[4],` +
		`"precipitation_sum":[1.5],"precipitation_probability_max":[40],"wind_speed_10m_max":[20]}}`
}

func serveJSON(t *testing.T, status int, body string, seen *atomic.Value) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			seen.Store(r.URL.String())
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func repoint(t *testing.T, target *string, url string) {
	t.Helper()
	old := *target
	*target = url
	t.Cleanup(func() { *target = old })
}

func TestOpenMeteoParsesAndClampsTheReadings(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.Local)
	repoint(t, &OpenMeteoURL, serveJSON(t, 200, openMeteoFixture("2026-10-01T14:00", "2026-10-01T15:00"), nil))
	w, err := fetchOpenMeteo(context.Background(), Location{City: "Berlin", Lat: 52.5, Lon: 13.4}, now)
	if err != nil {
		t.Fatal(err)
	}
	c := w.Current
	if c.Temperature != 21.5 || c.Condition != CondLightRain || !c.IsDay || c.Humidity != 60 {
		t.Errorf("current = %+v", c)
	}
	// Clamps: cloud cover 140 → 100, uv 20 → 15, direction 370 → 10, a
	// negative gust → 0, visibility in meters → km.
	if c.CloudCover != 100 || c.UVIndex != 15 || c.WindDirection != 10 || c.WindGust != 0 || c.Visibility != 24 {
		t.Errorf("clamped = cloud %d uv %d dir %d gust %v vis %v", c.CloudCover, c.UVIndex, c.WindDirection, c.WindGust, c.Visibility)
	}
	if len(w.Hourly) != 2 || w.Hourly[1].Time.Hour() != 15 {
		t.Errorf("hourly = %d entries", len(w.Hourly))
	}
	if len(w.Daily) != 1 || w.Daily[0].TempAvg != 18 || w.Daily[0].Condition != CondOvercast {
		t.Errorf("daily = %+v", w.Daily)
	}
	if w.Astronomy.Sunrise.String() != "07:12" || w.Astronomy.Sunset.String() != "18:45" {
		t.Errorf("astronomy = %v %v", w.Astronomy.Sunrise, w.Astronomy.Sunset)
	}
	if w.Location.City != "Berlin" {
		t.Errorf("location = %+v", w.Location)
	}
}

func TestOpenMeteoMissingDataIsAParseError(t *testing.T) {
	body := `{"hourly":{"time":["2026-10-01T14:00"],"temperature_2m":[]},"daily":{"time":[]}}`
	repoint(t, &OpenMeteoURL, serveJSON(t, 200, body, nil))
	_, err := fetchOpenMeteo(context.Background(), Location{}, time.Date(2026, 10, 1, 14, 30, 0, 0, time.Local))
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrOther || e.Retryable() || !strings.Contains(err.Error(), "missing data") {
		t.Fatalf("err = %v, want a non-retryable missing-data parse error", err)
	}
}

func TestHTTPStatusesMapToTheRustErrors(t *testing.T) {
	for _, tc := range []struct {
		status    int
		auth      bool
		kind      ErrorKind
		retryable bool
	}{
		{429, false, ErrRateLimited, true},
		{503, false, ErrOther, true},
		{404, false, ErrOther, false},
		{401, true, ErrAPIKeyMissing, false},
		{403, true, ErrAPIKeyMissing, false},
		// Open-Meteo has no key: its 401 is an ordinary status error.
		{401, false, ErrOther, false},
	} {
		url := serveJSON(t, tc.status, `{}`, nil)
		err := getJSON(context.Background(), "p", url, nil, tc.auth, &struct{}{})
		var e *Error
		if !errors.As(err, &e) || e.Kind != tc.kind || e.Retryable() != tc.retryable {
			t.Errorf("status %d auth %v: err = %v (kind %v retryable %v)", tc.status, tc.auth, err, e.Kind, e.Retryable())
		}
	}
}

func TestVisualCrossingSkipsPastHoursAndReadsIcons(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.Local)
	hour := func(at, icon string) string {
		return `{"datetime":"` + at + `","temp":10,"feelslike":9,"humidity":50,"dew":3,"windspeed":5,"winddir":90,` +
			`"pressure":1000,"visibility":10,"cloudcover":20,"uvindex":1,"icon":"` + icon + `","precipprob":70}`
	}
	body := `{"currentConditions":{"datetime":"20:00:00","temp":11,"feelslike":10,"humidity":40,"dew":2,"windspeed":4,` +
		`"winddir":180,"pressure":1001,"visibility":9,"cloudcover":10,"uvindex":0,"icon":"clear-night","sunrise":"07:00:00","sunset":"19:00:00"},` +
		`"days":[{"datetime":"2026-10-01","tempmax":15,"tempmin":5,"temp":10,"feelslike":9,"humidity":60,"dew":3,"windspeed":7,` +
		`"winddir":0,"pressure":1002,"cloudcover":30,"visibility":10,"uvindex":3,"sunrise":"07:00:00","sunset":"19:00:00",` +
		`"icon":"thunder-rain","hours":[` + hour("14:00:00", "rain") + `,` + hour("15:00:00", "snow-showers-day") + `]}]}`
	var seen atomic.Value
	repoint(t, &VisualCrossingURL, serveJSON(t, 200, body, &seen))
	w, err := fetchVisualCrossing(context.Background(), "k3y", City("New York"), Location{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if u := seen.Load().(string); !strings.Contains(u, "/New%20York?") || !strings.Contains(u, "key=k3y") {
		t.Errorf("request = %s", u)
	}
	if w.Current.Condition != CondClear || w.Current.IsDay {
		t.Errorf("20:00 after a 19:00 sunset: condition %v day %v", w.Current.Condition, w.Current.IsDay)
	}
	if len(w.Hourly) != 1 || w.Hourly[0].Condition != CondLightSnow || w.Hourly[0].RainChance != 70 {
		t.Errorf("hourly = %+v; 14:00 is past and skipped", w.Hourly)
	}
	if len(w.Daily) != 1 || w.Daily[0].TempAvg != 10 || w.Daily[0].Condition != CondThunderstorm {
		t.Errorf("daily = %+v", w.Daily)
	}
}

func TestWeatherAPIParses12HourSunTimes(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.Local)
	h := func(at string, code int) string {
		return fmt.Sprintf(`{"time":%q,"temp_c":8,"is_day":1,"condition":{"code":%d},"wind_kph":3,"wind_degree":45,`+
			`"pressure_mb":990,"precip_mm":0,"humidity":80,"cloud":90,"feelslike_c":6,"vis_km":5,"uv":2,"gust_kph":9,"dewpoint_c":4,"chance_of_rain":55}`, at, code)
	}
	body := `{"current":` + h("", 1009) + `,"forecast":{"forecastday":[{"date":"2026-10-01",` +
		`"day":{"maxtemp_c":10,"mintemp_c":2,"avgtemp_c":7,"maxwind_kph":15,"totalprecip_mm":3,"avghumidity":70,"daily_chance_of_rain":60,"condition":{"code":1195},"uv":2},` +
		`"astro":{"sunrise":"06:58 AM","sunset":"06:41 pm"},"hour":[` + h("2026-10-01 14:00", 1183) + `,` + h("2026-10-01 15:00", 1279) + `]}]}}`
	repoint(t, &WeatherAPIURL, serveJSON(t, 200, body, nil))
	w, err := fetchWeatherAPI(context.Background(), "k", Coords(1, 2), Location{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if w.Current.Condition != CondOvercast || w.Daily[0].Condition != CondHeavyRain || w.Daily[0].TempAvg != 7 {
		t.Errorf("current %v daily %+v", w.Current.Condition, w.Daily[0])
	}
	if w.Astronomy.Sunrise.String() != "06:58" || w.Astronomy.Sunset.String() != "18:41" {
		t.Errorf("sun = %v %v", w.Astronomy.Sunrise, w.Astronomy.Sunset)
	}
	if len(w.Hourly) != 1 || w.Hourly[0].Condition != CondThunderstorm {
		t.Errorf("hourly = %+v", w.Hourly)
	}
}

func TestKeyedProvidersWithoutAKeyFailFast(t *testing.T) {
	repoint(t, &GeocodingURL, serveJSON(t, 200, `{"results":[{"latitude":1,"longitude":2,"name":"X","country":"Y"}]}`, nil))
	for _, p := range []ProviderKind{VisualCrossing, WeatherAPI} {
		_, err := fetchWeather(context.Background(), Settings{Provider: p, Location: City("X")}, time.Now())
		if kindOf(err) != ErrAPIKeyMissing {
			t.Errorf("provider %v without a key: err = %v", p, err)
		}
	}
}

func TestUnknownCityIsLocationNotFound(t *testing.T) {
	repoint(t, &GeocodingURL, serveJSON(t, 200, `{}`, nil))
	_, err := resolve(context.Background(), City("Atlantis"))
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrLocationNotFound || e.Query != "Atlantis" {
		t.Fatalf("err = %v", err)
	}
	// Coordinates never geocode.
	if loc, err := resolve(context.Background(), Coords(3, 4)); err != nil || loc.Lat != 3 || loc.City != "" {
		t.Fatalf("coords = %+v, %v", loc, err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestServiceRetriesOnlyRetryableFailures(t *testing.T) {
	var calls atomic.Int32
	flaky := func(context.Context, Settings, time.Time) (*Weather, error) {
		if calls.Add(1) < 3 {
			return nil, rateLimited("p")
		}
		return &Weather{Location: Location{City: "ok"}}, nil
	}
	svc := newService(Settings{Interval: time.Hour}, flaky, func(error, int) time.Duration { return time.Millisecond })
	defer svc.Close()
	waitFor(t, "the third attempt to land", func() bool { return svc.Status().Kind == Loaded })
	if calls.Load() != 3 || svc.Weather().Location.City != "ok" {
		t.Errorf("calls %d weather %+v", calls.Load(), svc.Weather())
	}

	calls.Store(0)
	fatal := func(context.Context, Settings, time.Time) (*Weather, error) {
		calls.Add(1)
		return nil, apiKeyMissing("p")
	}
	svc2 := newService(Settings{Interval: time.Hour}, fatal, func(error, int) time.Duration { return time.Millisecond })
	defer svc2.Close()
	waitFor(t, "the failure", func() bool { return svc2.Status().Kind == Failed })
	if calls.Load() != 1 || svc2.Status().Error != ErrAPIKeyMissing || svc2.Weather() != nil {
		t.Errorf("a missing key was retried (%d calls) or mis-reported %+v", calls.Load(), svc2.Status())
	}
}

func TestServicePollsWithSubscribersOnlyAfterTheFirstFetch(t *testing.T) {
	var calls atomic.Int32
	count := func(context.Context, Settings, time.Time) (*Weather, error) {
		calls.Add(1)
		return &Weather{}, nil
	}
	svc := newService(Settings{Interval: 5 * time.Millisecond}, count, retryDelay)
	defer svc.Close()
	waitFor(t, "the startup fetch", func() bool { return calls.Load() >= 1 })
	time.Sleep(40 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetched %d times with nobody subscribed, want only the first", n)
	}
	_, stop := svc.Subscribe()
	defer stop()
	waitFor(t, "a subscribed poll", func() bool { return calls.Load() >= 2 })
}

func TestConfigureRestartsOnlyOnChange(t *testing.T) {
	var calls atomic.Int32
	count := func(context.Context, Settings, time.Time) (*Weather, error) {
		calls.Add(1)
		return &Weather{}, nil
	}
	s := Settings{Interval: time.Hour, Location: City("A")}
	svc := newService(s, count, retryDelay)
	defer svc.Close()
	waitFor(t, "the first fetch", func() bool { return calls.Load() == 1 })
	svc.Configure(s)
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("unchanged settings refetched")
	}
	s.Location = City("B")
	svc.Configure(s)
	waitFor(t, "the refetch for the new location", func() bool { return calls.Load() == 2 })
}

func TestRetryDelayBacksOffAndWaitsOutRateLimits(t *testing.T) {
	if d := retryDelay(httpError("p", errors.New("x")), 1); d != 5*time.Second {
		t.Errorf("attempt 1 = %v", d)
	}
	if d := retryDelay(httpError("p", errors.New("x")), 2); d != 10*time.Second {
		t.Errorf("attempt 2 = %v", d)
	}
	if d := retryDelay(rateLimited("p"), 1); d != time.Minute {
		t.Errorf("rate limit = %v", d)
	}
}

func TestUVRiskBands(t *testing.T) {
	for uv, want := range map[UVIndex]string{0: "Low", 2: "Low", 3: "Moderate", 6: "High", 8: "Very High", 11: "Extreme"} {
		if got := uv.RiskLevel(); got != want {
			t.Errorf("uv %d = %q, want %q", uv, got, want)
		}
	}
}

func TestRefreshRefetchesWithTheSameSettings(t *testing.T) {
	var calls atomic.Int32
	count := func(context.Context, Settings, time.Time) (*Weather, error) {
		calls.Add(1)
		return &Weather{}, nil
	}
	svc := newService(Settings{Interval: time.Hour}, count, retryDelay)
	defer svc.Close()
	waitFor(t, "the first fetch", func() bool { return calls.Load() == 1 })
	svc.Refresh()
	waitFor(t, "the refetch", func() bool { return calls.Load() == 2 })
}

func TestFailedStatusCarriesTheProviderAndQuery(t *testing.T) {
	if st := failedStatus(apiKeyMissing("weatherapi")); st.Kind != Failed || st.Error != ErrAPIKeyMissing || st.Provider != "weatherapi" {
		t.Errorf("key status = %+v", st)
	}
	if st := failedStatus(locationNotFound("Atlantis")); st.Error != ErrLocationNotFound || st.Query != "Atlantis" {
		t.Errorf("location status = %+v", st)
	}
	if st := failedStatus(errors.New("plain")); st.Error != ErrOther {
		t.Errorf("plain error status = %+v", st)
	}
}
