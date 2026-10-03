package bar

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
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

// arrangeDropdown measures and lays a dropdown out at a fixed panel
// size over two passes — the first settles parenting, the second the
// rebuilt subtrees' bounds.
func arrangeDropdown(t *testing.T, v widget.Widget, w, h int) {
	t.Helper()
	for range 2 {
		sz := v.Measure(widget.Constraints{Max: widget.Size{W: w, H: h}})
		v.Arrange(render.Rect{W: max(sz.W, w), H: h})
	}
}

// classedWidget finds the first widget carrying class, failing the
// test when it is missing.
func classedWidget(t *testing.T, root widget.Widget, class string) widget.Widget {
	t.Helper()
	w := findByClass(root, class)
	if w == nil {
		t.Fatalf("no %s widget under the tree", class)
	}
	return w
}

// classedBox is classedWidget for boxes.
func classedBox(t *testing.T, root widget.Widget, class string) *widget.Box {
	t.Helper()
	w := classedWidget(t, root, class)
	b, ok := w.(*widget.Box)
	if !ok {
		t.Fatalf("the %s widget is %T, want a box", class, w)
	}
	return b
}

// abs is the integer distance helper the bounds assertions share.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// The loading and error pages fill the stack and center their
// content; the loaded page keeps its sections at the top.
func TestWeatherPagesCenterTheirContent(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	// A fetch that never lands holds the loading page up.
	svc := weather.NewWith(weather.Settings{Interval: time.Hour}, func(ctx context.Context, _ weather.Settings, _ time.Time) (*weather.Weather, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	defer svc.Close()
	ctx.Weather = svc
	v := weatherDropdown(ctx).(*weatherView)
	if got := v.pages.Visible(); got != "loading" {
		t.Fatalf("page = %q, want loading", got)
	}
	arrangeDropdown(t, v, 400, 600)
	page := classedBox(t, v, "loading-weather")
	icon, isIcon := classedWidget(t, v, "loading-icon").(*widget.Icon)
	if !isIcon {
		t.Fatal("the loading icon is not an icon")
	}
	text := classedWidget(t, v, "loading-text").(*widget.Label)
	pb := page.Bounds()
	// The icon-over-text group centers; the icon alone sits half the
	// text above the middle.
	ib := icon.Bounds()
	group := ib.Y + (text.Bounds().Y+text.Bounds().H-ib.Y)/2
	if abs(group-(pb.Y+pb.H/2)) > 3 {
		t.Errorf("the loading group centers at %d, the page at %d", group, pb.Y+pb.H/2)
	}
	// The loading text ellipsizes like the Rust label.
	if text.Ellipsize() != widget.EllipsizeEnd {
		t.Error("the loading text does not ellipsize")
	}

	// The error page centers its group and the retry button.
	fail := fixedWeather(nil, &weather.Error{Kind: weather.ErrLocationNotFound, Query: "Atlantis"})
	defer fail.Close()
	waitHeadless(t, "the failed status", func() bool { return fail.Status().Kind == weather.Failed })
	ctx.Weather = fail
	v = weatherDropdown(ctx).(*weatherView)
	arrangeDropdown(t, v, 400, 600)
	page = classedBox(t, v, "error-weather")
	retry, isBtn := classedWidget(t, v, "weather-retry-btn").(*widget.Button)
	if !isBtn {
		t.Fatal("no retry button")
	}
	rb := retry.Bounds()
	if center := rb.X + rb.W/2; abs(center-(page.Bounds().X+page.Bounds().W/2)) > 3 {
		t.Errorf("the retry centers at %d, the page at %d", center, page.Bounds().X+page.Bounds().W/2)
	}
	if l := classedWidget(t, v, "error-title").(*widget.Label); l.Ellipsize() != widget.EllipsizeEnd {
		t.Error("the error title does not ellipsize")
	}
}

// Every refetch crossfades to the loading page — after a success too
// — and the loaded page keeps its sections at the top until the new
// data lands.
func TestWeatherRefetchShowsTheLoadingPage(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	w := &weather.Weather{
		Current: weather.Current{Temperature: 11, Condition: weather.CondRain},
		Hourly:  make([]weather.Hourly, 8),
		Daily:   []weather.Daily{{Date: time.Now(), TempLow: 4, TempHigh: 12}},
	}
	var mu sync.Mutex
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	calls := 0
	svc := weather.NewWith(weather.Settings{Interval: time.Hour}, func(context.Context, weather.Settings, time.Time) (*weather.Weather, error) {
		mu.Lock()
		i := calls
		calls++
		var gate chan struct{}
		if i < len(gates) {
			gate = gates[i]
		}
		mu.Unlock()
		if gate != nil {
			<-gate
		}
		return w, nil
	})
	defer svc.Close()
	ctx.Weather = svc
	v := weatherDropdown(ctx).(*weatherView)
	if got := v.pages.Visible(); got != "loading" {
		t.Fatalf("page = %q, want loading before the first fetch lands", got)
	}
	close(gates[0])
	waitHeadless(t, "the loaded status", func() bool { return svc.Status().Kind == weather.Loaded })
	v.refresh()
	if got := v.pages.Visible(); got != "loaded" {
		t.Fatalf("page = %q, want loaded", got)
	}
	// Negative: the loaded sections stay top-anchored — centering is
	// the loading and error pages' job.
	arrangeDropdown(t, v, 400, 600)
	scrollTop := classedWidget(t, v, "weather-scroll").(*widget.Scroll).Bounds().Y
	headerTop := classedBox(t, v, "weather-header").Bounds().Y
	if abs(headerTop-scrollTop) > 3 {
		t.Errorf("the loaded header sits at y=%d, the scroll content at %d", headerTop, scrollTop)
	}
	// A refetch after a success shows the loading page again.
	svc.Refresh()
	waitHeadless(t, "the refetching status", func() bool { return svc.Status().Kind == weather.Loading })
	v.refresh()
	if got := v.pages.Visible(); got != "loading" {
		t.Errorf("a refetch after a success kept page %q, want loading", got)
	}
	close(gates[1])
	waitHeadless(t, "the reloaded status", func() bool { return svc.Status().Kind == weather.Loaded })
	v.refresh()
	if got := v.pages.Visible(); got != "loaded" {
		t.Errorf("page = %q after the refetch landed, want loaded", got)
	}
}

// The header's refresh carries its i18n tooltip, the current weather
// rides in the weather-current/weather-temp-group wrappers, and the
// daily list wraps its items' high and low in daily-temps.
func TestWeatherHeaderAndDailyWrappers(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	svc := fixedWeather(&weather.Weather{
		Current:   weather.Current{Temperature: 11, Condition: weather.CondRain},
		Hourly:    make([]weather.Hourly, 8),
		Daily:     []weather.Daily{{Date: time.Now(), TempLow: 4, TempHigh: 12}},
		Astronomy: weather.Astronomy{Sunrise: weather.TimeOfDay{Hour: 6}, Sunset: weather.TimeOfDay{Hour: 18}},
	}, nil)
	defer svc.Close()
	waitHeadless(t, "the loaded status", func() bool { return svc.Status().Kind == weather.Loaded })
	ctx.Weather = svc
	v := weatherDropdown(ctx).(*weatherView)

	var refresh *widget.Button
	walkTree(v, func(w widget.Widget) bool {
		if b, isBtn := w.(*widget.Button); isBtn && b.HasClass("ghost-icon") {
			refresh = b
		}
		return true
	})
	if refresh == nil {
		t.Fatal("no refresh button")
	}
	if got := refresh.TooltipText(); got != i18n.T("dropdown-weather-refresh") {
		t.Errorf("refresh tooltip = %q, want %q", got, i18n.T("dropdown-weather-refresh"))
	}

	header := classedBox(t, v, "weather-header")
	if got := len(header.Children()); got != 2 {
		t.Fatalf("weather-header children = %d, want the current group and the location", got)
	}
	current, _ := header.Children()[0].(*widget.Box)
	if current == nil || !current.HasClass("weather-current") {
		t.Fatalf("weather-header's first child %v lacks weather-current", header.Children()[0])
	}
	var tempGroup *widget.Box
	for _, c := range current.Children() {
		if b, isBox := c.(*widget.Box); isBox && b.HasClass("weather-temp-group") {
			tempGroup = b
		}
		// Negative: the temp labels ride the temp group, not the bare
		// current row.
		if l, isLabel := c.(*widget.Label); isLabel && l.HasClass("weather-temp") {
			t.Error("the temperature label is a bare child of weather-current")
		}
	}
	if tempGroup == nil {
		t.Fatal("no weather-temp-group wrapper")
	}

	list, ok := findByClass(v, "daily-forecast").(*widget.Box)
	if !ok {
		t.Fatal("no daily-forecast list: the daily items hang off the section")
	}
	if len(list.Children()) != 1 {
		t.Fatalf("daily items = %d, want 1", len(list.Children()))
	}
	item, _ := list.Children()[0].(*widget.Box)
	var temps *widget.Box
	for _, c := range item.Children() {
		if b, isBox := c.(*widget.Box); isBox && b.HasClass("daily-temps") {
			temps = b
		}
		if l, isLabel := c.(*widget.Label); isLabel && (l.HasClass("daily-high") || l.HasClass("daily-low")) {
			t.Error("a daily temperature is a bare child of the daily item")
		}
	}
	if temps == nil {
		t.Fatal("no daily-temps wrapper")
	}
	if got := len(temps.Children()); got != 2 {
		t.Errorf("daily-temps children = %d, want the high and the low", got)
	}

	// The spacing-0 sites: the CSS rules carry the gaps.
	for _, tc := range []struct {
		what string
		got  int
	}{
		{"sun-times", classedBox(t, v, "sun-times").Spacing()},
		{"weather-header", header.Spacing()},
		{"weather-current", current.Spacing()},
		{"weather-temp-group", tempGroup.Spacing()},
		{"daily-forecast", list.Spacing()},
		{"daily-item", item.Spacing()},
	} {
		if tc.got != 0 {
			t.Errorf("%s spacing = %d, want 0", tc.what, tc.got)
		}
	}
	// Negative: the temp row keeps the Rust literal spacing of 2.
	var tempRow *widget.Box
	for _, c := range tempGroup.Children() {
		if b, isBox := c.(*widget.Box); isBox && findByClass(b, "weather-temp") != nil {
			tempRow = b
		}
	}
	if tempRow == nil || tempRow.Spacing() != 2 {
		t.Errorf("the temp row spacing = %v, want the literal 2", tempRow)
	}
}
