package weather

import (
	"context"
	"log"
	"net/url"
	"strconv"
	"time"
)

const openMeteoProvider = "open-meteo"

const (
	openMeteoHourlyParams = "temperature_2m,relative_humidity_2m,apparent_temperature," +
		"precipitation_probability,precipitation,weather_code,cloud_cover,pressure_msl," +
		"visibility,wind_speed_10m,wind_direction_10m,wind_gusts_10m,dew_point_2m,uv_index,is_day"
	openMeteoDailyParams = "weather_code,temperature_2m_max,temperature_2m_min," +
		"relative_humidity_2m_mean,sunrise,sunset,uv_index_max,precipitation_sum," +
		"precipitation_probability_max,wind_speed_10m_max"
)

// openMeteoResponse is open_meteo/types.rs ApiResponse: parallel arrays
// per hour and per day.
type openMeteoResponse struct {
	Hourly openMeteoHourly `json:"hourly"`
	Daily  openMeteoDaily  `json:"daily"`
}

type openMeteoHourly struct {
	Time                     []string  `json:"time"`
	Temperature2m            []float64 `json:"temperature_2m"`
	RelativeHumidity2m       []float64 `json:"relative_humidity_2m"`
	ApparentTemperature      []float64 `json:"apparent_temperature"`
	PrecipitationProbability []float64 `json:"precipitation_probability"`
	Precipitation            []float64 `json:"precipitation"`
	WeatherCode              []float64 `json:"weather_code"`
	CloudCover               []float64 `json:"cloud_cover"`
	PressureMsl              []float64 `json:"pressure_msl"`
	Visibility               []float64 `json:"visibility"`
	WindSpeed10m             []float64 `json:"wind_speed_10m"`
	WindDirection10m         []float64 `json:"wind_direction_10m"`
	WindGusts10m             []float64 `json:"wind_gusts_10m"`
	DewPoint2m               []float64 `json:"dew_point_2m"`
	UVIndex                  []float64 `json:"uv_index"`
	IsDay                    []float64 `json:"is_day"`
}

type openMeteoDaily struct {
	Time                        []string  `json:"time"`
	WeatherCode                 []float64 `json:"weather_code"`
	Temperature2mMax            []float64 `json:"temperature_2m_max"`
	Temperature2mMin            []float64 `json:"temperature_2m_min"`
	RelativeHumidity2mMean      []float64 `json:"relative_humidity_2m_mean"`
	Sunrise                     []string  `json:"sunrise"`
	Sunset                      []string  `json:"sunset"`
	UVIndexMax                  []float64 `json:"uv_index_max"`
	PrecipitationSum            []float64 `json:"precipitation_sum"`
	PrecipitationProbabilityMax []float64 `json:"precipitation_probability_max"`
	WindSpeed10mMax             []float64 `json:"wind_speed_10m_max"`
}

// fetchOpenMeteo is OpenMeteo::fetch: metric units, the location's own
// timezone, seven days.
func fetchOpenMeteo(ctx context.Context, resolved Location, now time.Time) (*Weather, error) {
	query := url.Values{
		"latitude":         {strconv.FormatFloat(resolved.Lat, 'f', -1, 64)},
		"longitude":        {strconv.FormatFloat(resolved.Lon, 'f', -1, 64)},
		"hourly":           {openMeteoHourlyParams},
		"daily":            {openMeteoDailyParams},
		"temperature_unit": {"celsius"},
		"wind_speed_unit":  {"kmh"},
		"timezone":         {"auto"},
		"forecast_days":    {"7"},
	}
	var data openMeteoResponse
	if err := getJSON(ctx, openMeteoProvider, OpenMeteoURL, query, false, &data); err != nil {
		return nil, err
	}
	return parseOpenMeteo(&data, resolved, now)
}

// parseOpenMeteo builds the model: the current conditions are the
// hour in progress, the hourly forecast the 24 hours from it.
func parseOpenMeteo(data *openMeteoResponse, resolved Location, now time.Time) (*Weather, error) {
	h := &data.Hourly
	start := currentHourIndex(h.Time, now)
	current, err := openMeteoHour(h, start)
	if err != nil {
		return nil, err
	}
	end := min(start+24, len(h.Time))
	hourly := make([]Hourly, 0, end-start)
	for i := start; i < end; i++ {
		hour, err := openMeteoHour(h, i)
		if err != nil {
			return nil, err
		}
		if hour.Time, err = parseLocal(openMeteoProvider, "2006-01-02T15:04", h.Time[i]); err != nil {
			return nil, err
		}
		hourly = append(hourly, hour)
	}
	daily, err := openMeteoDays(data, 7)
	if err != nil {
		return nil, err
	}
	return buildWeather(current.current(), hourly, daily, resolved, now), nil
}

// current drops the forecast-only fields.
func (h Hourly) current() Current {
	return Current{
		Temperature: h.Temperature, FeelsLike: h.FeelsLike, Condition: h.Condition,
		Humidity: h.Humidity, WindSpeed: h.WindSpeed, WindDirection: h.WindDirection,
		WindGust: h.WindGust, UVIndex: h.UVIndex, CloudCover: h.CloudCover,
		Pressure: h.Pressure, Visibility: h.Visibility, Dewpoint: h.Dewpoint,
		Precipitation: h.Precipitation, IsDay: h.IsDay,
	}
}

// currentHourIndex is find_current_hour_index: the hour before the
// first one still ahead of now, or 0 when the forecast lags behind.
func currentHourIndex(times []string, now time.Time) int {
	for i, stamp := range times {
		t, err := time.ParseInLocation("2006-01-02T15:04", stamp, now.Location())
		if err == nil && t.After(now) {
			return max(i-1, 0)
		}
	}
	log.Printf("weather: no future hour found in forecast, using fallback index 0")
	return 0
}

// openMeteoHour reads hour i of every array; a missing value is a
// parse error ("missing data").
func openMeteoHour(h *openMeteoHourly, i int) (Hourly, error) {
	r := arrayReader{provider: openMeteoProvider, i: i}
	out := Hourly{
		Condition:     FromWmoCode(r.u8(h.WeatherCode)),
		Humidity:      percentage(r.f(h.RelativeHumidity2m)),
		WindDirection: windDirection(r.f(h.WindDirection10m)),
		RainChance:    percentage(r.f(h.PrecipitationProbability)),
		UVIndex:       uvIndex(r.f(h.UVIndex)),
		CloudCover:    percentage(r.f(h.CloudCover)),
		IsDay:         r.f(h.IsDay) > 0.5,
	}
	p := openMeteoProvider
	out.Temperature = r.temp(p, r.f(h.Temperature2m))
	out.FeelsLike = r.temp(p, r.f(h.ApparentTemperature))
	out.Dewpoint = r.temp(p, r.f(h.DewPoint2m))
	out.WindSpeed = r.speed(p, r.f(h.WindSpeed10m))
	out.WindGust = r.speed(p, r.f(h.WindGusts10m))
	out.Pressure = r.pressure(p, r.f(h.PressureMsl))
	out.Visibility = r.km(p, r.f(h.Visibility)/1000)
	out.Precipitation = r.precip(p, r.f(h.Precipitation))
	return out, r.err
}

// openMeteoDays is build_daily: the first count days, each with its
// sun times and the mean of the high and low.
func openMeteoDays(data *openMeteoResponse, count int) ([]Daily, error) {
	d := &data.Daily
	n := min(len(d.Time), count)
	days := make([]Daily, 0, n)
	for i := range n {
		r := arrayReader{provider: openMeteoProvider, i: i}
		day := Daily{
			Condition:   FromWmoCode(r.u8(d.WeatherCode)),
			HumidityAvg: percentage(r.f(d.RelativeHumidity2mMean)),
			RainChance:  percentage(r.f(d.PrecipitationProbabilityMax)),
			UVIndexMax:  uvIndex(r.f(d.UVIndexMax)),
		}
		p := openMeteoProvider
		day.TempHigh = r.temp(p, r.f(d.Temperature2mMax))
		day.TempLow = r.temp(p, r.f(d.Temperature2mMin))
		day.TempAvg = (day.TempHigh + day.TempLow) / 2
		day.WindSpeedMax = r.speed(p, r.f(d.WindSpeed10mMax))
		day.PrecipitationSum = r.precip(p, r.f(d.PrecipitationSum))
		if r.err != nil {
			return nil, r.err
		}
		var err error
		if day.Date, err = parseLocal(openMeteoProvider, "2006-01-02", d.Time[i]); err != nil {
			return nil, err
		}
		if day.Sunrise, err = openMeteoTimeOfDay(d.Sunrise, i); err != nil {
			return nil, err
		}
		if day.Sunset, err = openMeteoTimeOfDay(d.Sunset, i); err != nil {
			return nil, err
		}
		days = append(days, day)
	}
	return days, nil
}

func openMeteoTimeOfDay(values []string, i int) (TimeOfDay, error) {
	if i >= len(values) {
		return TimeOfDay{}, parseError(openMeteoProvider, "missing data")
	}
	t, err := parseLocal(openMeteoProvider, "2006-01-02T15:04", values[i])
	if err != nil {
		return TimeOfDay{}, err
	}
	return TimeOfDay{Hour: t.Hour(), Minute: t.Minute()}, nil
}

// parseLocal parses a provider's naive local timestamp.
func parseLocal(provider, layout, value string) (time.Time, error) {
	t, err := time.ParseInLocation(layout, value, time.Local)
	if err != nil {
		return time.Time{}, parseError(provider, err.Error())
	}
	return t, nil
}

// arrayReader reads index i of parallel arrays; a short array is a
// parse error, kept with the conversion errors so a row reads straight
// through.
type arrayReader struct {
	fieldErrors
	provider string
	i        int
}

func (r *arrayReader) f(values []float64) float64 {
	if r.i >= len(values) {
		r.keep(parseError(r.provider, "missing data"))
		return 0
	}
	return values[r.i]
}

func (r *arrayReader) u8(values []float64) uint8 {
	return uint8(max(0, min(255, r.f(values))))
}
