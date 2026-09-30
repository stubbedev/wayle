package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		if got := Cardinal(tc.deg); got != tc.want {
			t.Errorf("cardinal(%d) = %q, want %q", tc.deg, got, tc.want)
		}
	}
}

func TestFindCurrentHourIndex(t *testing.T) {
	stamp := func(offset time.Duration) string {
		return time.Now().Add(offset).Format("2006-01-02T15:04")
	}
	times := []string{stamp(-2 * time.Hour), stamp(-1 * time.Hour), stamp(1 * time.Hour)}
	// The first future hit minus one.
	if got := findCurrentHourIndex(times); got != 1 {
		t.Errorf("index = %d, want 1", got)
	}
	// Everything in the past falls back to 0.
	past := []string{stamp(-3 * time.Hour), stamp(-2 * time.Hour)}
	if got := findCurrentHourIndex(past); got != 0 {
		t.Errorf("past index = %d, want 0", got)
	}
	// A future first hour clamps to 0.
	future := []string{stamp(1 * time.Hour)}
	if got := findCurrentHourIndex(future); got != 0 {
		t.Errorf("future index = %d, want 0", got)
	}
}

func TestGeocode(t *testing.T) {
	original := GeocodingURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/geocode" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"results":[{"latitude":37.77,"longitude":-122.42}]}`))
	}))
	t.Cleanup(server.Close)
	GeocodingURL = server.URL + "/geocode"
	t.Cleanup(func() { GeocodingURL = original })

	client := &Client{HTTP: server.Client()}
	lat, lon, err := client.Geocode(context.Background(), "San Francisco")
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if lat != 37.77 || lon != -122.42 {
		t.Errorf("lat=%f lon=%f", lat, lon)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[]}`))
	}))
	t.Cleanup(empty.Close)
	GeocodingURL = empty.URL + "/geocode"
	if _, _, err := client.Geocode(context.Background(), "Nowhere"); err == nil {
		t.Error("empty results: want an error")
	}
}

func TestFetchForecast(t *testing.T) {
	original := ForecastURL
	t.Cleanup(func() { ForecastURL = original })

	// Hour 0 is the past hour, hour 1 the future: current reads hour 0.
	body := `{
	  "hourly": {
	    "time": ["2020-01-01T10:00", "2030-01-01T11:00"],
	    "temperature_2m": [18.4, 19.0],
	    "relative_humidity_2m": [62, 60],
	    "apparent_temperature": [17.9, 18.5],
	    "weather_code": [61, 2],
	    "wind_speed_10m": [12.3, 13],
	    "wind_direction_10m": [90, 91]
	  },
	  "daily": {
	    "temperature_2m_max": [21.5],
	    "temperature_2m_min": [12.25]
	  }
	}`
	mux := http.NewServeMux()
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"hourly": {"time": ["2020-01-01T10:00"]}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ForecastURL = server.URL + "/forecast"
	client := &Client{HTTP: server.Client()}
	current, err := client.FetchForecast(context.Background(), 37.77, -122.42)
	if err != nil {
		t.Fatalf("FetchForecast: %v", err)
	}
	if current.TempC != 18.4 || current.FeelsLikeC != 17.9 {
		t.Errorf("temps = %+v, want hour 0", current)
	}
	if current.Condition != CondLightRain {
		t.Errorf("condition = %v, want light rain (wmo 61)", current.Condition)
	}
	if current.Humidity != 62 || current.WindKmh != 12.3 || current.WindDir != 90 {
		t.Errorf("wind/humidity = %+v", current)
	}
	if !current.HasHigh || current.HighC != 21.5 || !current.HasLow || current.LowC != 12.25 {
		t.Errorf("daily = %+v", current)
	}

	ForecastURL = server.URL + "/empty"
	if _, err := client.FetchForecast(context.Background(), 0, 0); err == nil {
		t.Error("forecast without temperature: want an error")
	}

	ForecastURL = server.URL + "/missing"
	if _, err := client.FetchForecast(context.Background(), 0, 0); err == nil {
		t.Error("HTTP 404: want an error")
	}
}
