package bar

import (
	"context"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/weather"
)

func TestWeatherSettingsMapsTheConfig(t *testing.T) {
	t.Setenv("WAYLE_TEST_WEATHER_KEY", "s3cret")
	cfg := config.Defaults().Weather
	cfg.Location = "52.5,13.4"
	cfg.Provider = config.WeatherProviderWeatherApi
	ref := "$WAYLE_TEST_WEATHER_KEY"
	cfg.WeatherapiKey = &ref
	cfg.RefreshIntervalSeconds = 90
	s := weatherSettings(cfg)
	if s.Provider != weather.WeatherAPI || !s.Location.Coordinates || s.Interval != 90*time.Second {
		t.Errorf("settings = %+v", s)
	}
	if s.WeatherAPIKey != "s3cret" {
		t.Errorf("weatherapi key = %q, want the resolved $WAYLE_TEST_WEATHER_KEY", s.WeatherAPIKey)
	}
	if s.VisualCrossingKey != "" {
		t.Errorf("an unset key resolved to %q", s.VisualCrossingKey)
	}
	// An unset variable leaves the key empty (the provider then reports
	// the missing key) rather than sending "$NAME" as the key.
	missing := "$WAYLE_TEST_WEATHER_UNSET"
	cfg.WeatherapiKey = &missing
	if got := weatherSettings(cfg).WeatherAPIKey; got != "" {
		t.Errorf("unset variable key = %q", got)
	}
}

func TestWeatherConditionIconsHaveDayAndNight(t *testing.T) {
	if weatherConditionIcon(weather.CondClear, true) != "ld-sun-symbolic" || weatherConditionIcon(weather.CondClear, false) != "ld-moon-symbolic" {
		t.Error("clear day/night icons")
	}
	if weatherConditionIcon(weather.CondRain, true) != weatherConditionIcon(weather.CondRain, false) {
		t.Error("rain has a single glyph")
	}
	if weatherConditionIcon(weather.CondUnknown, true) != "ld-cloud-symbolic" {
		t.Error("unknown condition icon")
	}
	if weatherConditionClass(weather.CondHail) != "stormy" || weatherConditionClass(weather.CondMist) != "cloudy" {
		t.Error("condition classes")
	}
}

func TestWeatherTimeLabels(t *testing.T) {
	at := time.Date(2026, 10, 1, 15, 0, 0, 0, time.Local)
	if got := weatherHourLabel(at, config.TimeFormat12h); got != "3PM" {
		t.Errorf("12h hour = %q", got)
	}
	if got := weatherHourLabel(at, config.TimeFormat24h); got != "15:00" {
		t.Errorf("24h hour = %q", got)
	}
	for _, tc := range []struct {
		at   weather.TimeOfDay
		want string
	}{
		{weather.TimeOfDay{Hour: 0, Minute: 5}, "12:05 AM"},
		{weather.TimeOfDay{Hour: 12, Minute: 0}, "12:00 PM"},
		{weather.TimeOfDay{Hour: 18, Minute: 41}, "6:41 PM"},
	} {
		if got := weatherSunTime(tc.at, config.TimeFormat12h); got != tc.want {
			t.Errorf("12h %v = %q, want %q", tc.at, got, tc.want)
		}
	}
	if got := weatherSunTime(weather.TimeOfDay{Hour: 6, Minute: 5}, config.TimeFormat24h); got != "06:05" {
		t.Errorf("24h sun = %q", got)
	}
}

func TestWeatherBarOffsets(t *testing.T) {
	if l, w := weatherBarOffsets(5, 10, 0, 20); l != 25 || w != 25 {
		t.Errorf("span = %v %v, want 25 25", l, w)
	}
	// A one-degree day still shows at least 5%.
	if _, w := weatherBarOffsets(10, 10.1, 0, 100); w != 5 {
		t.Errorf("min width = %v", w)
	}
	// A flat week fills the bar.
	if l, w := weatherBarOffsets(3, 3, 3, 3); l != 0 || w != 100 {
		t.Errorf("flat = %v %v", l, w)
	}
	if lo, hi := weatherTempRange(nil); lo != 0 || hi != 0 {
		t.Errorf("empty range = %v %v", lo, hi)
	}
	lo, hi := weatherTempRange([]weather.Daily{{TempLow: 2, TempHigh: 9}, {TempLow: -1, TempHigh: 7}})
	if lo != -1 || hi != 9 {
		t.Errorf("range = %v %v", lo, hi)
	}
}

func TestWeatherErrorTextNamesTheCause(t *testing.T) {
	if got := weatherErrorText(weather.Status{Kind: weather.Failed, Error: weather.ErrAPIKeyMissing, Provider: "weatherapi"}); got != i18n.T("dropdown-weather-error-api-key", i18n.Str("provider", "weatherapi")) {
		t.Errorf("api key = %q", got)
	}
	if got := weatherErrorText(weather.Status{Kind: weather.Failed, Error: weather.ErrLocationNotFound, Query: "Atlantis"}); got != i18n.T("dropdown-weather-error-location", i18n.Str("query", "Atlantis")) {
		t.Errorf("location = %q", got)
	}
	if got := weatherErrorText(weather.Status{Kind: weather.Failed}); got != i18n.T("dropdown-weather-error-unknown") {
		t.Errorf("other = %q", got)
	}
	if weatherPage(weather.Status{Kind: weather.Loading}) != "loading" || weatherPage(weather.Status{Kind: weather.Failed}) != "error" {
		t.Error("pages")
	}
	if got := weatherUpdatedAgo(time.Now().Add(time.Minute), time.Now()); got != i18n.T("dropdown-weather-updated-ago", i18n.Str("minutes", "0")) {
		t.Errorf("future update = %q, want 0 minutes", got)
	}
}

func TestDropdownDimsResolveTheOverrides(t *testing.T) {
	cfg := config.Defaults()
	cfg.Styling.Scale = 1
	if w, h, ok := dropdownDims("weather", cfg); !ok || w != 395 || h != 695 {
		t.Errorf("weather base = %d×%d %v", w, h, ok)
	}
	// A content-height dropdown has a natural height.
	if _, h, _ := dropdownDims("calendar", cfg); h != -1 {
		t.Errorf("calendar height = %d, want natural", h)
	}
	if _, _, ok := dropdownDims("power", cfg); ok {
		t.Error("the power menu has no panel geometry")
	}
	cfg.Styling.Scale = 2
	scale := config.Size{Value: 1.5}
	px := config.Size{Value: 480, Unit: config.SizePixels}
	cfg.Dropdowns.Weather = config.DropdownSize{Width: &scale, Height: &px}
	if w, h, _ := dropdownDims("weather", cfg); w != 1185 || h != 480 {
		t.Errorf("weather override = %d×%d, want 1185×480", w, h)
	}
	// A scale height on a content-height dropdown has nothing to scale.
	cfg.Dropdowns.Calendar = config.DropdownSize{Height: &scale}
	if w, h, _ := dropdownDims("calendar", cfg); w != 680 || h != -1 {
		t.Errorf("calendar = %d×%d, want 680×natural", w, h)
	}
	cfg.Dropdowns.Calendar = config.DropdownSize{Height: &px}
	if _, h, _ := dropdownDims("calendar", cfg); h != 480 {
		t.Errorf("calendar px height = %d", h)
	}
}

func fixedWeather(w *weather.Weather, err error) *weather.Service {
	return weather.NewWith(weather.Settings{Interval: time.Hour}, func(context.Context, weather.Settings, time.Time) (*weather.Weather, error) {
		return w, err
	})
}

func TestWeatherModuleFollowsTheService(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	if _, err := newWeather(ctx); err == nil {
		t.Fatal("a weather module without the service was built")
	}
	svc := fixedWeather(&weather.Weather{Current: weather.Current{Temperature: 20, Condition: weather.CondClear, IsDay: false}}, nil)
	defer svc.Close()
	ctx.Weather = svc
	ctx.gen = newMountGen()
	defer ctx.gen.retire()
	m, err := newWeather(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wm := m.(*weatherModule)
	waitForText(t, wm.label, "20°C")
	waitHeadless(t, "the night icon", func() bool { return wm.icon.Name() == "ld-moon-symbolic" })
}

func TestWeatherDropdownShowsTheStatusPage(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	svc := fixedWeather(nil, &weather.Error{Kind: weather.ErrLocationNotFound, Query: "Atlantis"})
	defer svc.Close()
	ctx.Weather = svc
	var v *weatherView
	waitHeadless(t, "the failed status", func() bool { return svc.Status().Kind == weather.Failed })
	v = weatherDropdown(ctx).(*weatherView)
	if got := v.pages.Visible(); got != "error" {
		t.Errorf("page = %q, want error", got)
	}
	if got := v.errorText.Text(); got != i18n.T("dropdown-weather-error-location", i18n.Str("query", "Atlantis")) {
		t.Errorf("error text = %q", got)
	}

	ok := fixedWeather(&weather.Weather{
		Current: weather.Current{Temperature: 11, Condition: weather.CondRain},
		Hourly:  make([]weather.Hourly, 8),
		Daily:   []weather.Daily{{Date: time.Now(), TempLow: 4, TempHigh: 12}},
	}, nil)
	defer ok.Close()
	ctx.Weather = ok
	waitHeadless(t, "the loaded status", func() bool { return ok.Status().Kind == weather.Loaded })
	v = weatherDropdown(ctx).(*weatherView)
	if got := v.pages.Visible(); got != "loaded" {
		t.Errorf("page = %q, want loaded", got)
	}
	// Header, stats, hourly, daily, and sun times.
	if got := len(v.loaded.Children()); got != 5 {
		t.Errorf("loaded sections = %d, want 5", got)
	}
	hourly := v.loaded.Children()[2].(*widget.Box).Children()[1].(*widget.Box)
	if got := len(hourly.Children()); got != weatherHourlyItems {
		t.Errorf("hourly items = %d, want %d of 8", got, weatherHourlyItems)
	}
}
