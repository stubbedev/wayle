package bar

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/weather"
	"github.com/stubbedev/wayle/styling"
)

// The dropdown's list lengths and the temperature bar track
// (hourly_forecast MAX_ITEMS, daily_forecast MAX_DAYS, BAR_WIDTH_REM).
const (
	weatherHourlyItems  = 5
	weatherDailyDays    = 5
	weatherBarWidthRem  = 4.0
	weatherUpdatedEvery = time.Minute
)

// weatherHourLabel is hourly_time_label: "3PM" or "15:00".
func weatherHourLabel(t time.Time, format config.TimeFormat) string {
	if format == config.TimeFormat24h {
		return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
	}
	return t.Format("3PM")
}

// weatherSunTime is sun_times format_time: "6:05 AM" or "06:05".
func weatherSunTime(t weather.TimeOfDay, format config.TimeFormat) string {
	if format == config.TimeFormat24h {
		return t.String()
	}
	period := "AM"
	if t.Hour >= 12 {
		period = "PM"
	}
	hour := t.Hour % 12
	if hour == 0 {
		hour = 12
	}
	return fmt.Sprintf("%d:%02d %s", hour, t.Minute, period)
}

// weatherDayLabel is day_label: the weekday's abbreviation.
func weatherDayLabel(date time.Time) string {
	ids := [...]string{
		"dropdown-weather-day-sun", "dropdown-weather-day-mon", "dropdown-weather-day-tue",
		"dropdown-weather-day-wed", "dropdown-weather-day-thu", "dropdown-weather-day-fri",
		"dropdown-weather-day-sat",
	}
	return i18n.T(ids[date.Weekday()])
}

// weatherLocationDisplay is location_display: "city, region", else
// "city, country".
func weatherLocationDisplay(l weather.Location) string {
	if l.Region != "" {
		return l.City + ", " + l.Region
	}
	return l.City + ", " + l.Country
}

// weatherUpdatedAgo is the header's "Updated Nm ago" (never negative).
func weatherUpdatedAgo(updated, now time.Time) string {
	minutes := max(int64(now.Sub(updated)/time.Minute), 0)
	return i18n.T("dropdown-weather-updated-ago", i18n.Str("minutes", strconv.FormatInt(minutes, 10)))
}

// weatherTempRange is temp_range: the coldest low and warmest high,
// (0, 0) without days.
func weatherTempRange(days []weather.Daily) (lo, hi float32) {
	if len(days) == 0 {
		return 0, 0
	}
	lo, hi = math.MaxFloat32, -math.MaxFloat32
	for _, d := range days {
		lo = min(lo, d.TempLow.Celsius())
		hi = max(hi, d.TempHigh.Celsius())
	}
	return lo, hi
}

// weatherBarOffsets is temp_bar_offsets: where a day's low-to-high
// span sits on the week's range, in percent, at least 5% wide; a flat
// range fills the bar.
func weatherBarOffsets(low, high, rangeMin, rangeMax float32) (leftPct, widthPct float32) {
	span := rangeMax - rangeMin
	if span <= 0 {
		return 0, 100
	}
	clamp := func(v float32) float32 { return max(0, min(100, v)) }
	left := clamp((low - rangeMin) / span * 100)
	right := clamp((high - rangeMin) / span * 100)
	return left, max(right-left, 5)
}

// weatherErrorText is error_description.
func weatherErrorText(st weather.Status) string {
	switch st.Error {
	case weather.ErrAPIKeyMissing:
		return i18n.T("dropdown-weather-error-api-key", i18n.Str("provider", st.Provider))
	case weather.ErrLocationNotFound:
		return i18n.T("dropdown-weather-error-location", i18n.Str("query", st.Query))
	case weather.ErrNetwork:
		return i18n.T("dropdown-weather-error-network")
	case weather.ErrRateLimited:
		return i18n.T("dropdown-weather-error-rate-limit")
	}
	return i18n.T("dropdown-weather-error-unknown")
}

// weatherPage is the page a status shows.
func weatherPage(st weather.Status) string {
	switch st.Kind {
	case weather.Loaded:
		return "loaded"
	case weather.Failed:
		return "error"
	}
	return "loading"
}

// weatherView is the weather dropdown (dropdowns/weather): a loading,
// error, or loaded page following the service while open.
type weatherView struct {
	ctx  ModuleContext
	svc  *weather.Service
	font render.Font
	px   float64

	*widget.Box
	pages     *widget.Stack
	errorText *widget.Label
	loaded    *widget.Box

	once sync.Once
	stop chan struct{}
}

func weatherDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	// The Dropdown template box is spacing-0: the header strip and the
	// .dropdown-content rules carry every gap.
	v := &weatherView{ctx: ctx, svc: ctx.Weather, font: font, px: px, stop: make(chan struct{})}
	v.Box = widget.NewBox(widget.Column, 0, 0)
	v.AddClass("dropdown", "weather-dropdown")
	v.Append(v.header(), false)
	v.pages = widget.NewStack()
	v.pages.SetTransition(widget.StackCrossfade, gtkStackDuration)
	v.pages.Add("loading", v.loadingPage())
	v.pages.Add("error", v.errorPage())
	v.loaded = widget.NewBox(widget.Column, 0, 0)
	// The loaded child is vexpand inside the scroll (weather/mod.rs):
	// the sections fill the viewport, top-anchored.
	scroll := dropdownScroll(v.loaded, "weather-scroll")
	scroll.FillY = true
	v.pages.Add("loaded", scroll)
	// DropdownContent: the content box the stylesheet's .dropdown-content
	// rules hang off (default ink, the section-label family).
	content := widget.NewBox(widget.Column, 0, 0)
	content.AddClass("dropdown-content")
	content.Append(v.pages, true)
	v.Append(content, true)
	v.refresh()
	v.follow()
	return v
}

// icon sizes a theme icon and carries the rule's classes; its ink
// follows the cascade color, and the stylesheet's -gtk-icon-size
// overrides the constructor size.
func (v *weatherView) icon(name string, scale float64, classes ...string) *widget.Icon {
	icon := widget.NewThemeIcon(name, int(math.Round(v.px*scale)))
	icon.AddClass(classes...)
	return icon
}

// label paints with the constructor ink unset (0): the classed rule
// the class selects supplies the color.
func (v *weatherView) label(text string, scale float64, class string) *widget.Label {
	l := widget.NewLabel(v.font, v.px*scale, text, 0)
	l.AddClass(class)
	return l
}

// header is the DropdownHeader: the title and the refresh action.
func (v *weatherView) header() widget.Widget {
	refresh := widget.NewButton(v.icon("tb-refresh-symbolic", 1), 6, 6)
	refresh.AddClass("ghost-icon")
	refresh.OnClick = v.svc.Refresh
	refresh.SetTooltip(i18n.T("dropdown-weather-refresh"))
	return dropdownHeader(v.font, v.px, "ld-sun-symbolic", i18n.T("dropdown-weather-title"), refresh)
}

// vcentered fills a column page's height and centers its kids as a
// group — a filling stack page's valign Center, with each kid at the
// natural size the page's halign Fill renders centered.
func vcentered(col *widget.Box, kids ...widget.Widget) *widget.Box {
	col.Append(widget.NewBox(widget.Column, 0, 0), true)
	for _, k := range kids {
		col.AppendAligned(k, false, widget.AlignCenter)
	}
	col.Append(widget.NewBox(widget.Column, 0, 0), true)
	return col
}

func (v *weatherView) loadingPage() widget.Widget {
	col := widget.NewBox(widget.Column, 0, 0)
	col.AddClass("loading-weather")
	text := v.label(i18n.T("dropdown-weather-loading"), 1, "loading-text")
	text.SetEllipsize(widget.EllipsizeEnd)
	vcentered(col, v.icon("ld-sun-symbolic", 2.5, "loading-icon"), text)
	return col
}

func (v *weatherView) errorPage() widget.Widget {
	col := widget.NewBox(widget.Column, 0, 0)
	col.AddClass("error-weather")
	title := v.label(i18n.T("dropdown-weather-error-title"), 1.1, "error-title")
	title.SetEllipsize(widget.EllipsizeEnd)
	v.errorText = v.label("", 0.95, "error-text")
	v.errorText.SetEllipsize(widget.EllipsizeEnd)
	retry := widget.NewButton(widget.NewLabel(v.font, v.px, i18n.T("dropdown-weather-retry"), 0), 8, 6)
	retry.AddClass("weather-retry-btn")
	retry.OnClick = v.svc.Refresh
	vcentered(col, v.icon("ld-info-symbolic", 2.5, "error-icon"), title, v.errorText, retry)
	return col
}

// refresh shows the status's page and rebuilds the loaded sections
// from the latest data (every section's refresh at once). Every
// refetch crossfades to the loading page, after a success too; the
// loaded page keeps its last render until the new data lands.
func (v *weatherView) refresh() {
	if v.svc == nil {
		// No service: nothing to read (the Rust factory never builds the
		// dropdown without one; the builders sweep must not panic).
		return
	}
	st := v.svc.Status()
	w := v.svc.Weather()
	v.errorText.SetText(weatherErrorText(st))
	v.pages.Show(weatherPage(st))
	if w == nil {
		return
	}
	cfg := v.ctx.Config.Weather
	imperial := cfg.Units == config.TemperatureUnitImperial
	v.loaded.Clear()
	v.loaded.Append(v.currentHeader(w, imperial), false)
	v.loaded.Append(v.statsGrid(w, imperial), false)
	v.loaded.Append(v.hourly(w, imperial, cfg.TimeFormat), false)
	v.loaded.Append(v.daily(w, imperial), false)
	v.loaded.Append(v.sunTimes(w, cfg.TimeFormat), false)
}

// currentHeader is weather_header: the condition icon, temperature,
// and condition inside .weather-current, with the place and data age
// in .weather-location on the right. Every label and the icon carry
// the class whose rule paints them.
func (v *weatherView) currentHeader(w *weather.Weather, imperial bool) widget.Widget {
	c := w.Current
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("weather-header")
	current := widget.NewBox(widget.Row, 0, 0)
	current.AddClass("weather-current")
	current.Append(v.icon(weatherConditionIcon(c.Condition, c.IsDay), 3, "weather-icon", weatherConditionClass(c.Condition)), false)
	temps := widget.NewBox(widget.Column, 0, 0)
	temps.AddClass("weather-temp-group")
	temp := widget.NewBox(widget.Row, 2, 0)
	temp.Append(v.label(weatherTemp(c.Temperature, imperial), 2.4, "weather-temp"), false)
	temp.Append(v.label(weatherUnitSymbol(imperial), 1.2, "weather-temp-unit"), false)
	temps.Append(temp, false)
	temps.Append(v.label(weatherConditionLabel(c.Condition), 1, "weather-condition"), false)
	current.Append(temps, false)
	row.Append(current, false)
	// weather-location is hexpand, valign Center: it takes the free
	// width and its right-aligned labels sit mid-height.
	place := widget.NewBox(widget.Column, 0, 0)
	place.AddClass("weather-location")
	city := v.label(weatherLocationDisplay(w.Location), 1, "weather-city")
	city.SetEllipsize(widget.EllipsizeEnd)
	city.SetAlignment(render.AlignEnd)
	place.Append(city, false)
	ago := v.label(weatherUpdatedAgo(w.UpdatedAt, time.Now()), 0.85, "weather-updated")
	ago.SetAlignment(render.AlignEnd)
	place.Append(ago, false)
	row.AppendAligned(place, true, widget.AlignCenter)
	return row
}

// statsGrid is stats_grid: humidity, wind, UV, and today's rain chance.
func (v *weatherView) statsGrid(w *weather.Weather, imperial bool) widget.Widget {
	rain := "--"
	if len(w.Daily) > 0 {
		rain = strconv.Itoa(int(w.Daily[0].RainChance)) + "%"
	}
	row := widget.NewBox(widget.Row, 6, 0)
	row.AddClass("weather-stats")
	stats := []struct {
		icon, value, label, class string
	}{
		{"ld-droplets-symbolic", strconv.Itoa(int(w.Current.Humidity)) + "%", "dropdown-weather-humidity", "humidity"},
		{"ld-wind-symbolic", weatherSpeed(w.Current.WindSpeed, imperial), "dropdown-weather-wind", "wind"},
		{"ld-sun-symbolic", strconv.Itoa(int(w.Current.UVIndex)), "dropdown-weather-uv", "uv"},
		{"ld-cloud-rain-symbolic", rain, "dropdown-weather-rain", "rain"},
	}
	for i, s := range stats {
		cell := widget.NewBox(widget.Column, 2, 6)
		cell.AddClass("weather-stat")
		setClass(cell, "stat-first", i == 0)
		setClass(cell, "stat-last", i == len(stats)-1)
		cell.Append(v.icon(s.icon, 1.3, "weather-stat-icon", s.class), false)
		cell.Append(v.label(s.value, 1, "weather-stat-value"), false)
		cell.Append(v.label(i18n.T(s.label), 0.8, "weather-stat-label"), false)
		row.Append(cell, true)
	}
	return row
}

// hourly is hourly_forecast: "Now" then the next four hours.
func (v *weatherView) hourly(w *weather.Weather, imperial bool, format config.TimeFormat) widget.Widget {
	col := widget.NewBox(widget.Column, 6, 0)
	col.AddClass("weather-section")
	col.Append(v.label(i18n.T("dropdown-weather-hourly"), 0.9, "section-label"), false)
	row := widget.NewBox(widget.Row, 4, 0)
	row.AddClass("hourly-forecast")
	for i, h := range w.Hourly[:min(len(w.Hourly), weatherHourlyItems)] {
		when := i18n.T("dropdown-weather-now")
		if i > 0 {
			when = weatherHourLabel(h.Time, format)
		}
		item := widget.NewBox(widget.Column, 4, 4)
		item.AddClass("hourly-item")
		item.Append(v.label(when, 0.85, "hourly-time"), false)
		item.Append(v.icon(weatherConditionIcon(h.Condition, h.IsDay), 1.4, "hourly-icon", weatherConditionClass(h.Condition)), false)
		item.Append(v.label(weatherTemp(h.Temperature, imperial)+"°", 1, "hourly-temp"), false)
		row.Append(item, true)
	}
	col.Append(row, false)
	return col
}

// daily is daily_forecast: five days in the .daily-forecast list, each
// with its span on the week's temperature range. The span is a
// .daily-bar track with a .daily-bar-fill the stylesheet paints; the
// fill's offset and width are per-day geometry, so they go inline —
// gelm has no widget-level min-width/margin-start to carry the Rust
// set_width_request/set_margin_start pair.
func (v *weatherView) daily(w *weather.Weather, imperial bool) widget.Widget {
	col := widget.NewBox(widget.Column, 0, 0)
	col.AddClass("weather-section")
	col.Append(v.label(i18n.T("dropdown-weather-daily"), 0.9, "section-label"), false)
	list := widget.NewBox(widget.Column, 0, 0)
	list.AddClass("daily-forecast")
	days := w.Daily[:min(len(w.Daily), weatherDailyDays)]
	lo, hi := weatherTempRange(days)
	scale := float64(v.ctx.Config.Styling.Scale)
	if scale <= 0 {
		scale = 1
	}
	barPx := int(math.Round(weatherBarWidthRem * scale * styling.RemBase))
	today := time.Now()
	for _, d := range days {
		isToday := d.Date.Year() == today.Year() && d.Date.YearDay() == today.YearDay()
		name := weatherDayLabel(d.Date)
		if isToday {
			name = i18n.T("dropdown-weather-today")
		}
		row := widget.NewBox(widget.Row, 0, 0)
		row.AddClass("daily-item")
		setClass(row, "today", isToday)
		row.Append(v.label(name, 1, "daily-day"), false)
		row.Append(v.icon(weatherConditionIcon(d.Condition, true), 1.2, "daily-icon", weatherConditionClass(d.Condition)), false)
		cond := v.label(weatherConditionLabel(d.Condition), 0.9, "daily-condition")
		cond.SetEllipsize(widget.EllipsizeEnd)
		row.Append(cond, true)
		left, width := weatherBarOffsets(d.TempLow.Celsius(), d.TempHigh.Celsius(), lo, hi)
		leftPx := int(left / 100 * float32(barPx))
		fillPx := min(int(max(width/100*float32(barPx), 3)), barPx-leftPx)
		bar := widget.NewBox(widget.Row, 0, 0)
		bar.AddClass("daily-bar")
		bar.SetInlineStyle(fmt.Sprintf("min-width: %dpx;", barPx))
		fill := widget.NewBox(widget.Row, 0, 0)
		fill.AddClass("daily-bar-fill")
		fill.SetInlineStyle(fmt.Sprintf("margin-left: %dpx; min-width: %dpx;", leftPx, fillPx))
		bar.Append(fill, false)
		row.AppendAligned(bar, false, widget.AlignCenter)
		temps := widget.NewBox(widget.Row, 0, 0)
		temps.AddClass("daily-temps")
		temps.Append(v.label(weatherTemp(d.TempHigh, imperial)+"°", 1, "daily-high"), false)
		temps.Append(v.label(weatherTemp(d.TempLow, imperial)+"°", 1, "daily-low"), false)
		row.Append(temps, false)
		list.Append(row, false)
	}
	col.Append(list, false)
	return col
}

// sunTimes is sun_times: today's sunrise and sunset.
func (v *weatherView) sunTimes(w *weather.Weather, format config.TimeFormat) widget.Widget {
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("sun-times")
	for i, s := range []struct {
		icon, label, class string
		at                 weather.TimeOfDay
	}{
		{"ld-sunrise-symbolic", "dropdown-weather-sunrise", "sunrise", w.Astronomy.Sunrise},
		{"ld-sunset-symbolic", "dropdown-weather-sunset", "sunset", w.Astronomy.Sunset},
	} {
		cell := widget.NewBox(widget.Row, 0, 0)
		cell.AddClass("sun-time")
		cell.Append(v.icon(s.icon, 1.4, "sun-icon", s.class), false)
		info := widget.NewBox(widget.Column, 0, 0)
		info.AddClass("sun-info")
		info.Append(v.label(i18n.T(s.label), 0.8, "sun-label"), false)
		info.Append(v.label(weatherSunTime(s.at, format), 1, "sun-value"), false)
		cell.Append(info, false)
		row.Append(cell, i == 0)
	}
	return row
}

// follow keeps the view current while open: service changes rebuild
// it, and the data age re-renders every minute (UPDATED_AGO_INTERVAL).
func (v *weatherView) follow() {
	if v.ctx.App == nil {
		return
	}
	changes, unsubscribe := v.svc.Subscribe()
	go func() {
		defer unsubscribe()
		ticker := time.NewTicker(weatherUpdatedEvery)
		defer ticker.Stop()
		for {
			select {
			case <-v.stop:
				return
			case <-changes:
			case <-ticker.C:
			}
			v.ctx.Invoke(v.refresh)
		}
	}()
}

// dropdownClosed implements dropdownCloser.
func (v *weatherView) dropdownClosed() { v.once.Do(func() { close(v.stop) }) }
