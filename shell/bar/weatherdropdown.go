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

// weatherConditionToken is the condition tint (the .sunny/.cloudy/...
// rules shared by the header, hourly, and daily icons).
func weatherConditionToken(c weather.Condition) config.CssToken {
	switch weatherConditionClass(c) {
	case "sunny":
		return config.TokenStatusWarning
	case "rainy":
		return config.TokenStatusInfo
	case "stormy":
		return config.TokenAccent
	case "snowy":
		return config.TokenFgDefault
	}
	return config.TokenFgSubtle
}

func tokenColor(palette *styling.Palette, token config.CssToken) render.Color {
	color, _ := styling.ResolveColor(config.ColorValue{Token: token}, palette)
	return color
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
	if ctx.Weather == nil {
		col := widget.NewBox(widget.Column, 6, 14)
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-weather-error-unknown"), mutedFg(ctx.Style.palette)), false)
		return col
	}
	v := &weatherView{ctx: ctx, svc: ctx.Weather, font: font, px: px, stop: make(chan struct{})}
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "weather-dropdown")
	v.Append(v.header(), false)
	v.pages = widget.NewStack()
	v.pages.Add("loading", v.loadingPage())
	v.pages.Add("error", v.errorPage())
	v.loaded = widget.NewBox(widget.Column, 14, 0)
	v.pages.Add("loaded", widget.NewScroll(v.loaded))
	v.Append(v.pages, true)
	v.refresh()
	v.follow()
	return v
}

func (v *weatherView) muted() render.Color { return mutedFg(v.ctx.Style.palette) }

func (v *weatherView) tint(token config.CssToken) render.Color {
	return tokenColor(v.ctx.Style.palette, token)
}

func (v *weatherView) icon(name string, scale float64, token config.CssToken) *widget.Icon {
	icon := widget.NewThemeIcon(name, int(math.Round(v.px*scale)))
	icon.SetTint(v.tint(token))
	return icon
}

func (v *weatherView) label(text string, scale float64, color render.Color) *widget.Label {
	return widget.NewLabel(v.font, v.px*scale, text, color)
}

// header is the DropdownHeader: the title and the refresh action.
func (v *weatherView) header() widget.Widget {
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("dropdown-header")
	row.Append(v.icon("ld-sun-symbolic", 1.2, config.TokenFgDefault), false)
	row.Append(v.label(i18n.T("dropdown-weather-title"), 1.1, v.ctx.Style.fg), true)
	refresh := widget.NewButton(v.icon("tb-refresh-symbolic", 1, config.TokenFgMuted), 6, 6)
	refresh.AddClass("ghost-icon-button")
	refresh.BgHover = v.ctx.Style.buttonBgHover
	refresh.BgPressed = v.ctx.Style.buttonBgActive
	refresh.OnClick = v.svc.Refresh
	row.Append(refresh, false)
	return row
}

func (v *weatherView) loadingPage() widget.Widget {
	col := widget.NewBox(widget.Column, 8, 24)
	col.AddClass("loading-weather")
	col.Append(v.icon("ld-sun-symbolic", 2.5, config.TokenFgSubtle), false)
	col.Append(v.label(i18n.T("dropdown-weather-loading"), 1, v.muted()), false)
	return col
}

func (v *weatherView) errorPage() widget.Widget {
	col := widget.NewBox(widget.Column, 8, 24)
	col.AddClass("error-weather")
	col.Append(v.icon("ld-info-symbolic", 2.5, config.TokenFgSubtle), false)
	col.Append(v.label(i18n.T("dropdown-weather-error-title"), 1.1, v.ctx.Style.fg), false)
	v.errorText = v.label("", 0.95, v.muted())
	v.errorText.SetEllipsize(widget.EllipsizeEnd)
	col.Append(v.errorText, false)
	retry := widget.NewButton(v.label(i18n.T("dropdown-weather-retry"), 1, v.ctx.Style.fg), 8, 6)
	retry.AddClass("weather-retry-btn")
	retry.BgHover = v.ctx.Style.buttonBgHover
	retry.BgPressed = v.ctx.Style.buttonBgActive
	retry.OnClick = v.svc.Refresh
	col.Append(retry, false)
	return col
}

// refresh shows the status's page and rebuilds the loaded sections
// from the latest data (every section's refresh at once).
func (v *weatherView) refresh() {
	st := v.svc.Status()
	page := weatherPage(st)
	w := v.svc.Weather()
	// A refetch after a success keeps the last data on screen, as the
	// Rust sections keep their last render while loading.
	if page == "loading" && w != nil {
		page = "loaded"
	}
	v.errorText.SetText(weatherErrorText(st))
	v.pages.Show(page)
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
// and condition, with the place and data age on the right.
func (v *weatherView) currentHeader(w *weather.Weather, imperial bool) widget.Widget {
	c := w.Current
	row := widget.NewBox(widget.Row, 12, 0)
	row.AddClass("weather-header")
	row.Append(v.icon(weatherConditionIcon(c.Condition, c.IsDay), 3, weatherConditionToken(c.Condition)), false)
	temps := widget.NewBox(widget.Column, 2, 0)
	temp := widget.NewBox(widget.Row, 2, 0)
	temp.Append(v.label(weatherTemp(c.Temperature, imperial), 2.4, v.ctx.Style.fg), false)
	temp.Append(v.label(weatherUnitSymbol(imperial), 1.2, v.tint(config.TokenFgSubtle)), false)
	temps.Append(temp, false)
	temps.Append(v.label(weatherConditionLabel(c.Condition), 1, v.muted()), false)
	row.Append(temps, true)
	place := widget.NewBox(widget.Column, 2, 0)
	city := v.label(weatherLocationDisplay(w.Location), 1, v.ctx.Style.fg)
	city.SetEllipsize(widget.EllipsizeEnd)
	city.SetAlignment(render.AlignEnd)
	place.Append(city, false)
	ago := v.label(weatherUpdatedAgo(w.UpdatedAt, time.Now()), 0.85, v.tint(config.TokenFgSubtle))
	ago.SetAlignment(render.AlignEnd)
	place.Append(ago, false)
	row.Append(place, false)
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
	for _, s := range []struct {
		icon, value, label string
		token              config.CssToken
	}{
		{"ld-droplets-symbolic", strconv.Itoa(int(w.Current.Humidity)) + "%", "dropdown-weather-humidity", config.TokenStatusInfo},
		{"ld-wind-symbolic", weatherSpeed(w.Current.WindSpeed, imperial), "dropdown-weather-wind", config.TokenFgMuted},
		{"ld-sun-symbolic", strconv.Itoa(int(w.Current.UVIndex)), "dropdown-weather-uv", config.TokenStatusWarning},
		{"ld-cloud-rain-symbolic", rain, "dropdown-weather-rain", config.TokenStatusInfo},
	} {
		cell := widget.NewBox(widget.Column, 2, 6)
		cell.AddClass("weather-stat")
		cell.Append(v.icon(s.icon, 1.3, s.token), false)
		cell.Append(v.label(s.value, 1, v.ctx.Style.fg), false)
		cell.Append(v.label(i18n.T(s.label), 0.8, v.tint(config.TokenFgSubtle)), false)
		row.Append(cell, true)
	}
	return row
}

// hourly is hourly_forecast: "Now" then the next four hours.
func (v *weatherView) hourly(w *weather.Weather, imperial bool, format config.TimeFormat) widget.Widget {
	col := widget.NewBox(widget.Column, 6, 0)
	col.AddClass("weather-section")
	col.Append(v.label(i18n.T("dropdown-weather-hourly"), 0.9, v.muted()), false)
	row := widget.NewBox(widget.Row, 4, 0)
	row.AddClass("hourly-forecast")
	for i, h := range w.Hourly[:min(len(w.Hourly), weatherHourlyItems)] {
		when := i18n.T("dropdown-weather-now")
		if i > 0 {
			when = weatherHourLabel(h.Time, format)
		}
		item := widget.NewBox(widget.Column, 4, 4)
		item.AddClass("hourly-item")
		item.Append(v.label(when, 0.85, v.muted()), false)
		item.Append(v.icon(weatherConditionIcon(h.Condition, h.IsDay), 1.4, weatherConditionToken(h.Condition)), false)
		item.Append(v.label(weatherTemp(h.Temperature, imperial)+"°", 1, v.ctx.Style.fg), false)
		row.Append(item, true)
	}
	col.Append(row, false)
	return col
}

// daily is daily_forecast: five days, each with its span on the week's
// temperature range.
func (v *weatherView) daily(w *weather.Weather, imperial bool) widget.Widget {
	col := widget.NewBox(widget.Column, 4, 0)
	col.AddClass("weather-section")
	col.Append(v.label(i18n.T("dropdown-weather-daily"), 0.9, v.muted()), false)
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
		nameColor := v.ctx.Style.fg
		if isToday {
			name, nameColor = i18n.T("dropdown-weather-today"), v.tint(config.TokenAccent)
		}
		row := widget.NewBox(widget.Row, 8, 2)
		row.AddClass("daily-item")
		setClass(row, "today", isToday)
		row.Append(v.label(name, 1, nameColor), false)
		row.Append(v.icon(weatherConditionIcon(d.Condition, true), 1.2, weatherConditionToken(d.Condition)), false)
		cond := v.label(weatherConditionLabel(d.Condition), 0.9, v.muted())
		cond.SetEllipsize(widget.EllipsizeEnd)
		row.Append(cond, true)
		left, width := weatherBarOffsets(d.TempLow.Celsius(), d.TempHigh.Celsius(), lo, hi)
		row.Append(&tempBar{
			w: barPx, left: int(left / 100 * float32(barPx)), fill: int(max(width/100*float32(barPx), 3)),
			track: v.tint(config.TokenBgHover), from: v.tint(config.TokenStatusInfo), to: v.tint(config.TokenStatusWarning),
		}, false)
		row.Append(v.label(weatherTemp(d.TempHigh, imperial)+"°", 1, v.ctx.Style.fg), false)
		row.Append(v.label(weatherTemp(d.TempLow, imperial)+"°", 1, v.tint(config.TokenFgSubtle)), false)
		col.Append(row, false)
	}
	return col
}

// sunTimes is sun_times: today's sunrise and sunset.
func (v *weatherView) sunTimes(w *weather.Weather, format config.TimeFormat) widget.Widget {
	row := widget.NewBox(widget.Row, 12, 0)
	row.AddClass("sun-times")
	for i, s := range []struct {
		icon, label string
		at          weather.TimeOfDay
		token       config.CssToken
	}{
		{"ld-sunrise-symbolic", "dropdown-weather-sunrise", w.Astronomy.Sunrise, config.TokenStatusWarning},
		{"ld-sunset-symbolic", "dropdown-weather-sunset", w.Astronomy.Sunset, config.TokenAccent},
	} {
		cell := widget.NewBox(widget.Row, 8, 0)
		cell.AddClass("sun-time")
		cell.Append(v.icon(s.icon, 1.4, s.token), false)
		info := widget.NewBox(widget.Column, 2, 0)
		info.Append(v.label(i18n.T(s.label), 0.8, v.tint(config.TokenFgSubtle)), false)
		info.Append(v.label(weatherSunTime(s.at, format), 1, v.ctx.Style.fg), false)
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

// tempBar is the daily-bar: a rounded track with the day's span filled
// by the info-to-warning gradient.
type tempBar struct {
	widget.Base
	w, left, fill   int
	track, from, to render.Color
}

const tempBarHeightPx = 4

func (b *tempBar) Measure(widget.Constraints) widget.Size {
	return widget.Size{W: b.w, H: tempBarHeightPx}
}

func (b *tempBar) Arrange(r render.Rect) { b.ArrangeSelf(r) }

func (b *tempBar) Paint(cv *render.Canvas) {
	r := b.Bounds()
	track := render.Rect{X: r.X, Y: r.Y + (r.H-tempBarHeightPx)/2, W: b.w, H: tempBarHeightPx}
	cv.RoundedRect(track, tempBarHeightPx/2, b.track)
	fill := track
	fill.X += b.left
	fill.W = min(b.fill, b.w-b.left)
	cv.FillGradient(fill, render.UniformCorners(tempBarHeightPx/2), 90, []render.GradientStop{{Pos: 0, Color: b.from}, {Pos: 1, Color: b.to}})
}

func (b *tempBar) HitTest(widget.Point) widget.Widget { return nil }
