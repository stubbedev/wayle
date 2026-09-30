package bar

import (
	"context"
	"errors"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/weather"
	"github.com/stubbedev/wayle/strftime"
)

// worldClockRender is helpers.rs's format_world_clock: every
// {{ tz('Zone', 'strftime') }} renders the current instant in that
// zone; plain text passes through; a bad zone renders empty (the Rust
// warns and formats to "").
func worldClockRender(format string, now time.Time) string {
	var out strings.Builder
	rest := format
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end := strings.Index(rest[start:], "}}")
		if end < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end += start
		out.WriteString(rest[:start])
		call := strings.TrimSpace(rest[start+2 : end])
		if zoneID, layout, ok := parseTzCall(call); ok {
			out.WriteString(renderTz(zoneID, layout, now))
		}
		rest = rest[end+2:]
	}
}

// parseTzCall reads tz('Zone', 'strftime'); anything else is not a
// recognized call and renders as nothing.
func parseTzCall(call string) (string, string, bool) {
	const prefix = "tz("
	if !strings.HasPrefix(call, prefix) || !strings.HasSuffix(call, ")") {
		return "", "", false
	}
	inner := call[len(prefix) : len(call)-1]
	zone, args, found := strings.Cut(inner, ",")
	if !found {
		return "", "", false
	}
	zoneID, ok := singleQuoted(zone)
	if !ok {
		return "", "", false
	}
	layout, ok := singleQuoted(args)
	return zoneID, layout, ok
}

// singleQuoted extracts the '…' span of one argument.
func singleQuoted(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < 2 || arg[0] != '\'' {
		return "", false
	}
	end := strings.IndexByte(arg[1:], '\'')
	if end < 0 {
		return "", false
	}
	return arg[1 : 1+end], true
}

// renderTz formats now in the zone; an unknown zone renders empty.
func renderTz(zoneID, layout string, now time.Time) string {
	location, err := time.LoadLocation(zoneID)
	if err != nil {
		return ""
	}
	formatted, err := strftime.Compile(layout)
	if err != nil {
		return ""
	}
	return formatted.Format(now.In(location))
}

// weatherFormatLabel is helpers.rs's format_label over the schema's
// nine placeholders.
func weatherFormatLabel(format string, current weather.Current, imperial bool) string {
	temp, unit, speed := formatWeatherUnits(current, imperial)
	humidity := strconv.Itoa(current.Humidity) + "%"
	high, low := "", ""
	if current.HasHigh {
		high = tempString(current.HighC, imperial)
	}
	if current.HasLow {
		low = tempString(current.LowC, imperial)
	}
	out := replaceTemplateVar(format, "temp", temp)
	out = replaceTemplateVar(out, "temp_unit", unit)
	out = replaceTemplateVar(out, "feels_like", tempString(current.FeelsLikeC, imperial))
	out = replaceTemplateVar(out, "condition", current.Condition.Label())
	out = replaceTemplateVar(out, "humidity", humidity)
	out = replaceTemplateVar(out, "wind_speed", speed)
	out = replaceTemplateVar(out, "wind_dir", weather.Cardinal(current.WindDir))
	out = replaceTemplateVar(out, "high", high)
	out = replaceTemplateVar(out, "low", low)
	return out
}

// formatWeatherUnits renders the default format's pieces: the rounded
// temperature, its unit symbol, and the wind speed with its unit.
func formatWeatherUnits(current weather.Current, imperial bool) (temp, unit, speed string) {
	if imperial {
		return tempString(current.TempC, true), "°F",
			strconv.Itoa(int(math.Round(current.WindKmh*0.621371))) + " mph"
	}
	return tempString(current.TempC, false), "°C",
		strconv.Itoa(int(math.Round(current.WindKmh))) + " km/h"
}

// tempString is format_temp_value: rounded to a whole degree in the
// requested system.
func tempString(celsius float64, imperial bool) string {
	value := celsius
	if imperial {
		value = celsius*9.0/5.0 + 32.0
	}
	return strconv.Itoa(int(math.Round(value)))
}

// worldClock is the module: the format re-renders every second.
type worldClockModule struct {
	ctx   ModuleContext
	label *widget.Label
	stop  func()
}

func newWorldClock(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("world-clock: requires the application loop")
	}
	cfg := ctx.Config.WorldClock
	m := &worldClockModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.apply(cfg.Format)
	runCtx, cancel := context.WithCancel(context.Background())
	m.stop = cancel
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
			m.ctx.Invoke(func() { m.apply(m.ctx.Config.WorldClock.Format) })
		}
	}()
	return m, nil
}

// apply renders the current format; an unchanged label is a no-op for
// the damage collector.
func (m *worldClockModule) apply(format string) {
	text := ""
	if m.ctx.Config.WorldClock.LabelShow {
		text = worldClockRender(format, time.Now())
	}
	m.label.SetText(text)
}

func (m *worldClockModule) Root() widget.Widget {
	return assembleModule(m.ctx, moduleIcon(m.ctx, m.ctx.Config.WorldClock.Icon), m.label)
}

// Stop ends the render ticker.
func (m *worldClockModule) Stop() { m.stop() }

// weather is the module: the Open-Meteo conditions for one city.
type weatherModule struct {
	ctx    ModuleContext
	client *weather.Client
	label  *widget.Label
	cancel context.CancelFunc
}

func newWeather(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("weather: requires the application loop")
	}
	m := &weatherModule{ctx: ctx, client: weather.NewClient(), label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.refresh(runCtx)
	go func() {
		ticker := time.NewTicker(time.Duration(ctx.Config.Weather.RefreshS) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
			m.refresh(runCtx)
		}
	}()
	return m, nil
}

// refresh geocodes and fetches once; failures keep the previous label
// like the Rust watcher.
func (m *weatherModule) refresh(ctx context.Context) {
	cfg := m.ctx.Config.Weather
	lat, lon, err := m.client.Geocode(ctx, cfg.Location)
	if err != nil {
		log.Printf("weather: %v", err)
		return
	}
	current, err := m.client.FetchForecast(ctx, lat, lon)
	if err != nil {
		log.Printf("weather: %v", err)
		return
	}
	m.ctx.Invoke(func() {
		text := ""
		if cfg.LabelShow {
			text = weatherFormatLabel(cfg.Format, current, cfg.Units == config.WeatherImperial)
		}
		m.label.SetText(text)
	})
}

func (m *weatherModule) Root() widget.Widget {
	return assembleModule(m.ctx, moduleIcon(m.ctx, m.ctx.Config.Weather.Icon), m.label)
}

// Stop ends the refresh loop.
func (m *weatherModule) Stop() { m.cancel() }
