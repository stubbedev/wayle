package weather

import (
	"context"
	"net/url"
	"time"
)

const visualCrossingProvider = "visual-crossing"

// vcConditions are the fields a Visual Crossing current, hour, or day
// record shares (visual_crossing/types.rs).
type vcConditions struct {
	Datetime   string   `json:"datetime"`
	Temp       float64  `json:"temp"`
	Feelslike  float64  `json:"feelslike"`
	Humidity   float64  `json:"humidity"`
	Dew        float64  `json:"dew"`
	Precip     *float64 `json:"precip"`
	Precipprob *float64 `json:"precipprob"`
	Windspeed  float64  `json:"windspeed"`
	Winddir    float64  `json:"winddir"`
	Windgust   *float64 `json:"windgust"`
	Pressure   float64  `json:"pressure"`
	Visibility float64  `json:"visibility"`
	Cloudcover float64  `json:"cloudcover"`
	Uvindex    float64  `json:"uvindex"`
	Icon       string   `json:"icon"`
	Sunrise    string   `json:"sunrise"`
	Sunset     string   `json:"sunset"`
}

type vcDay struct {
	vcConditions
	Tempmax float64        `json:"tempmax"`
	Tempmin float64        `json:"tempmin"`
	Hours   []vcConditions `json:"hours"`
}

type vcResponse struct {
	CurrentConditions vcConditions `json:"currentConditions"`
	Days              []vcDay      `json:"days"`
}

// fetchVisualCrossing is VisualCrossing::fetch: the timeline API with
// the query in the path, metric, current + hours + days.
func fetchVisualCrossing(ctx context.Context, key string, q LocationQuery, resolved Location, now time.Time) (*Weather, error) {
	query := url.Values{"key": {key}, "unitGroup": {"metric"}, "include": {"current,hours,days"}, "iconSet": {"icons2"}}
	var data vcResponse
	if err := getJSON(ctx, visualCrossingProvider, VisualCrossingURL+"/"+url.PathEscape(q.providerPath()), query, true, &data); err != nil {
		return nil, err
	}
	return parseVisualCrossing(&data, resolved, now)
}

func parseVisualCrossing(data *vcResponse, resolved Location, now time.Time) (*Weather, error) {
	cur := data.CurrentConditions
	current, err := vcHour(cur, cur.Sunrise, cur.Sunset)
	if err != nil {
		return nil, err
	}
	var hourly []Hourly
	for _, day := range data.Days {
		date, err := parseLocal(visualCrossingProvider, "2006-01-02", day.Datetime)
		if err != nil {
			return nil, err
		}
		for _, h := range day.Hours {
			if len(hourly) >= 24 {
				break
			}
			tod, err := vcTime(h.Datetime)
			if err != nil {
				return nil, err
			}
			at := date.Add(time.Duration(tod.minutes()) * time.Minute)
			if at.Before(now) {
				continue
			}
			hour, err := vcHour(h, day.Sunrise, day.Sunset)
			if err != nil {
				return nil, err
			}
			hour.Time = at
			hourly = append(hourly, hour)
		}
		if len(hourly) >= 24 {
			break
		}
	}
	daily := make([]Daily, 0, min(len(data.Days), 7))
	for _, day := range data.Days[:min(len(data.Days), 7)] {
		d, err := vcDaily(day)
		if err != nil {
			return nil, err
		}
		daily = append(daily, d)
	}
	return buildWeather(current.current(), hourly, daily, resolved, now), nil
}

func vcHour(c vcConditions, sunrise, sunset string) (Hourly, error) {
	p := visualCrossingProvider
	out := Hourly{
		Condition:     vcCondition(c.Icon),
		Humidity:      percentage(c.Humidity),
		WindDirection: windDirection(c.Winddir),
		RainChance:    percentage(orZero(c.Precipprob)),
		UVIndex:       uvIndex(c.Uvindex),
		CloudCover:    percentage(c.Cloudcover),
		IsDay:         vcIsDay(c.Datetime, sunrise, sunset),
	}
	var errs fieldErrors
	out.Temperature = errs.temp(p, c.Temp)
	out.FeelsLike = errs.temp(p, c.Feelslike)
	out.Dewpoint = errs.temp(p, c.Dew)
	out.WindSpeed = errs.speed(p, c.Windspeed)
	out.WindGust = errs.speed(p, orZero(c.Windgust))
	out.Pressure = errs.pressure(p, c.Pressure)
	out.Visibility = errs.km(p, c.Visibility)
	out.Precipitation = errs.precip(p, orZero(c.Precip))
	return out, errs.err
}

func vcDaily(day vcDay) (Daily, error) {
	p := visualCrossingProvider
	out := Daily{
		Condition:   vcCondition(day.Icon),
		HumidityAvg: percentage(day.Humidity),
		RainChance:  percentage(orZero(day.Precipprob)),
		UVIndexMax:  uvIndex(day.Uvindex),
	}
	var errs fieldErrors
	out.TempHigh = errs.temp(p, day.Tempmax)
	out.TempLow = errs.temp(p, day.Tempmin)
	out.TempAvg = (out.TempHigh + out.TempLow) / 2
	out.WindSpeedMax = errs.speed(p, day.Windspeed)
	out.PrecipitationSum = errs.precip(p, orZero(day.Precip))
	if errs.err != nil {
		return Daily{}, errs.err
	}
	var err error
	if out.Date, err = parseLocal(p, "2006-01-02", day.Datetime); err != nil {
		return Daily{}, err
	}
	if out.Sunrise, err = vcTime(day.Sunrise); err != nil {
		return Daily{}, err
	}
	if out.Sunset, err = vcTime(day.Sunset); err != nil {
		return Daily{}, err
	}
	return out, nil
}

// vcTime parses HH:MM:SS or HH:MM.
func vcTime(s string) (TimeOfDay, error) {
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return TimeOfDay{Hour: t.Hour(), Minute: t.Minute()}, nil
		}
	}
	return TimeOfDay{}, parseError(visualCrossingProvider, "invalid time "+s)
}

// vcIsDay is is_daytime: between sunrise (inclusive) and sunset; any
// unparsable time counts as day.
func vcIsDay(current, sunrise, sunset string) bool {
	c, err1 := vcTime(current)
	rise, err2 := vcTime(sunrise)
	set, err3 := vcTime(sunset)
	if err1 != nil || err2 != nil || err3 != nil {
		return true
	}
	return c.minutes() >= rise.minutes() && c.minutes() < set.minutes()
}

// vcCondition is condition_from_icon over the icons2 set.
func vcCondition(icon string) Condition {
	switch icon {
	case "clear-day", "clear-night":
		return CondClear
	case "partly-cloudy-day", "partly-cloudy-night":
		return CondPartlyCloudy
	case "cloudy":
		return CondCloudy
	case "fog":
		return CondFog
	case "wind":
		return CondWindy
	case "rain":
		return CondRain
	case "showers-day", "showers-night":
		return CondLightRain
	case "thunder-rain", "thunder-showers-day", "thunder-showers-night":
		return CondThunderstorm
	case "snow":
		return CondSnow
	case "snow-showers-day", "snow-showers-night":
		return CondLightSnow
	case "sleet":
		return CondSleet
	case "hail":
		return CondHail
	}
	return CondUnknown
}

func orZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// fieldErrors converts a record's scalar readings, keeping the first
// error.
type fieldErrors struct{ err error }

func (e *fieldErrors) keep(err error) {
	if e.err == nil {
		e.err = err
	}
}

func (e *fieldErrors) temp(p string, v float64) Temperature {
	t, err := temperature(p, v)
	e.keep(err)
	return t
}

func (e *fieldErrors) speed(p string, v float64) Speed {
	s, err := speed(p, v)
	e.keep(err)
	return s
}

func (e *fieldErrors) pressure(p string, v float64) Pressure {
	x, err := pressure(p, v)
	e.keep(err)
	return x
}

func (e *fieldErrors) km(p string, v float64) Distance {
	d, err := distanceKm(p, v)
	e.keep(err)
	return d
}

func (e *fieldErrors) precip(p string, v float64) Precipitation {
	x, err := precipitation(p, v)
	e.keep(err)
	return x
}
