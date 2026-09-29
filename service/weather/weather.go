// Package weather is the Open-Meteo client behind the bar's weather
// module: forward geocoding for the configured city, the forecast
// request the Rust wayle-weather sends, and the WMO code mapping.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GeocodingURL and ForecastURL are the Open-Meteo endpoints; tests
// repoint them at a local server.
var (
	GeocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
	ForecastURL  = "https://api.open-meteo.com/v1/forecast"
)

// Condition is the model.rs WeatherCondition enum.
type Condition int

// Conditions.
const (
	CondClear Condition = iota
	CondPartlyCloudy
	CondCloudy
	CondOvercast
	CondMist
	CondFog
	CondLightRain
	CondRain
	CondHeavyRain
	CondDrizzle
	CondLightSnow
	CondSnow
	CondHeavySnow
	CondSleet
	CondThunderstorm
	CondWindy
	CondHail
	CondUnknown
)

// FromWmoCode maps an Open-Meteo WMO weather code.
func FromWmoCode(code uint8) Condition {
	switch code {
	case 0:
		return CondClear
	case 1, 2:
		return CondPartlyCloudy
	case 3:
		return CondOvercast
	case 45, 48:
		return CondFog
	case 51, 53, 55:
		return CondDrizzle
	case 56, 57:
		return CondSleet
	case 61:
		return CondLightRain
	case 63:
		return CondRain
	case 65:
		return CondHeavyRain
	case 66, 67:
		return CondSleet
	case 71:
		return CondLightSnow
	case 73:
		return CondSnow
	case 75:
		return CondHeavySnow
	case 77:
		return CondSnow
	case 80, 81, 82:
		return CondRain
	case 85, 86:
		return CondSnow
	case 95, 96, 99:
		return CondThunderstorm
	}
	return CondUnknown
}

// Label is the en-US condition vocabulary (_weather.ftl).
func (c Condition) Label() string {
	switch c {
	case CondClear:
		return "Clear"
	case CondPartlyCloudy:
		return "Partly Cloudy"
	case CondCloudy:
		return "Cloudy"
	case CondOvercast:
		return "Overcast"
	case CondMist:
		return "Mist"
	case CondFog:
		return "Fog"
	case CondLightRain:
		return "Light Rain"
	case CondRain:
		return "Rain"
	case CondHeavyRain:
		return "Heavy Rain"
	case CondDrizzle:
		return "Drizzle"
	case CondLightSnow:
		return "Light Snow"
	case CondSnow:
		return "Snow"
	case CondHeavySnow:
		return "Heavy Snow"
	case CondSleet:
		return "Sleet"
	case CondThunderstorm:
		return "Thunderstorm"
	case CondWindy:
		return "Windy"
	case CondHail:
		return "Hail"
	}
	return "Unknown"
}

// Cardinal is types.rs WindDirection::cardinal.
func Cardinal(deg uint16) string {
	switch {
	case deg <= 22 || deg >= 338:
		return "N"
	case deg <= 67:
		return "NE"
	case deg <= 112:
		return "E"
	case deg <= 157:
		return "SE"
	case deg <= 202:
		return "S"
	case deg <= 247:
		return "SW"
	case deg <= 292:
		return "W"
	}
	return "NW"
}

// Current is one conditions snapshot, in celsius and km/h.
type Current struct {
	TempC      float64
	FeelsLikeC float64
	Condition  Condition
	Humidity   int
	WindKmh    float64
	WindDir    uint16
	HighC      float64
	HasHigh    bool
	LowC       float64
	HasLow     bool
}

// Client fetches weather over HTTP.
type Client struct {
	HTTP *http.Client
}

// NewClient builds a client with sane timeouts.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Geocode resolves a city name to coordinates; the first hit wins,
// like the Rust count=1 query.
func (c *Client) Geocode(ctx context.Context, city string) (lat, lon float64, err error) {
	query := url.Values{"name": {city}, "count": {"1"}}
	var reply struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, GeocodingURL+"?"+query.Encode(), &reply); err != nil {
		return 0, 0, err
	}
	if len(reply.Results) == 0 {
		return 0, 0, fmt.Errorf("weather: no results for %q", city)
	}
	return reply.Results[0].Latitude, reply.Results[0].Longitude, nil
}

// fetchParams mirrors the Rust ForecastRequest: hourly arrays feed the
// current conditions, daily the high/low, and timezone=auto keeps the
// timestamps local to the city.
const (
	hourlyParams = "temperature_2m,relative_humidity_2m,apparent_temperature,weather_code,wind_speed_10m,wind_direction_10m,is_day"
	dailyParams  = "weather_code,temperature_2m_max,temperature_2m_min"
)

// FetchForecast pulls and assembles the current conditions.
func (c *Client) FetchForecast(ctx context.Context, lat, lon float64) (Current, error) {
	query := url.Values{
		"latitude":         {strconv.FormatFloat(lat, 'f', -1, 64)},
		"longitude":        {strconv.FormatFloat(lon, 'f', -1, 64)},
		"hourly":           {hourlyParams},
		"daily":            {dailyParams},
		"temperature_unit": {"celsius"},
		"wind_speed_unit":  {"kmh"},
		"timezone":         {"auto"},
		"forecast_days":    {"7"},
	}
	var reply struct {
		Hourly struct {
			Time                []string  `json:"time"`
			Temperature2m       []float64 `json:"temperature_2m"`
			RelativeHumidity2m  []float64 `json:"relative_humidity_2m"`
			ApparentTemperature []float64 `json:"apparent_temperature"`
			WeatherCode         []float64 `json:"weather_code"`
			WindSpeed10m        []float64 `json:"wind_speed_10m"`
			WindDirection10m    []float64 `json:"wind_direction_10m"`
		} `json:"hourly"`
		Daily struct {
			Temperature2mMax []float64 `json:"temperature_2m_max"`
			Temperature2mMin []float64 `json:"temperature_2m_min"`
		} `json:"daily"`
	}
	if err := c.getJSON(ctx, ForecastURL+"?"+query.Encode(), &reply); err != nil {
		return Current{}, err
	}
	idx := findCurrentHourIndex(reply.Hourly.Time)
	current := Current{
		Condition: CondUnknown,
	}
	var ok bool
	if current.TempC, ok = at(reply.Hourly.Temperature2m, idx); !ok {
		return Current{}, fmt.Errorf("weather: forecast has no temperature at hour %d", idx)
	}
	current.FeelsLikeC, _ = at(reply.Hourly.ApparentTemperature, idx)
	if code, ok := at(reply.Hourly.WeatherCode, idx); ok {
		current.Condition = FromWmoCode(uint8(code))
	}
	if humidity, ok := at(reply.Hourly.RelativeHumidity2m, idx); ok {
		current.Humidity = int(humidity)
	}
	if wind, ok := at(reply.Hourly.WindSpeed10m, idx); ok {
		current.WindKmh = wind
	}
	if dir, ok := at(reply.Hourly.WindDirection10m, idx); ok {
		current.WindDir = uint16(dir)
	}
	if high, ok := at(reply.Daily.Temperature2mMax, 0); ok {
		current.HighC, current.HasHigh = high, true
	}
	if low, ok := at(reply.Daily.Temperature2mMin, 0); ok {
		current.LowC, current.HasLow = low, true
	}
	return current, nil
}

// findCurrentHourIndex is parse.rs's: the first hour past now, one
// before it, falling back to 0 when the forecast lags behind.
func findCurrentHourIndex(times []string) int {
	now := time.Now().Format("2006-01-02T15:04")
	for idx, stamp := range times {
		if stamp > now {
			if idx > 0 {
				return idx - 1
			}
			return 0
		}
	}
	return 0
}

func at(values []float64, idx int) (float64, bool) {
	if idx < 0 || idx >= len(values) {
		return 0, false
	}
	return values[idx], true
}

func (c *Client) getJSON(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("weather: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("weather: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather: %s returned %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("weather: decode: %w", err)
	}
	return nil
}
