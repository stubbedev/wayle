package weather

import (
	"context"
	"net/url"
	"strings"
	"time"
)

const weatherAPIProvider = "weatherapi"

type waCondition struct {
	Code int `json:"code"`
}

// waHour is a WeatherAPI current or hourly record (weatherapi/types.rs).
type waHour struct {
	Time         string      `json:"time"`
	TempC        float64     `json:"temp_c"`
	IsDay        int         `json:"is_day"`
	Condition    waCondition `json:"condition"`
	WindKph      float64     `json:"wind_kph"`
	WindDegree   float64     `json:"wind_degree"`
	PressureMb   float64     `json:"pressure_mb"`
	PrecipMm     float64     `json:"precip_mm"`
	Humidity     float64     `json:"humidity"`
	Cloud        float64     `json:"cloud"`
	FeelslikeC   float64     `json:"feelslike_c"`
	VisKm        float64     `json:"vis_km"`
	UV           float64     `json:"uv"`
	GustKph      float64     `json:"gust_kph"`
	DewpointC    float64     `json:"dewpoint_c"`
	ChanceOfRain float64     `json:"chance_of_rain"`
}

type waForecastDay struct {
	Date string `json:"date"`
	Day  struct {
		MaxtempC          float64     `json:"maxtemp_c"`
		MintempC          float64     `json:"mintemp_c"`
		AvgtempC          float64     `json:"avgtemp_c"`
		MaxwindKph        float64     `json:"maxwind_kph"`
		TotalprecipMm     float64     `json:"totalprecip_mm"`
		Avghumidity       float64     `json:"avghumidity"`
		DailyChanceOfRain float64     `json:"daily_chance_of_rain"`
		Condition         waCondition `json:"condition"`
		UV                float64     `json:"uv"`
	} `json:"day"`
	Astro struct {
		Sunrise string `json:"sunrise"`
		Sunset  string `json:"sunset"`
	} `json:"astro"`
	Hour []waHour `json:"hour"`
}

type waResponse struct {
	Current  waHour `json:"current"`
	Forecast struct {
		Forecastday []waForecastDay `json:"forecastday"`
	} `json:"forecast"`
}

// fetchWeatherAPI is WeatherApi::fetch: seven days, no air quality or
// alerts.
func fetchWeatherAPI(ctx context.Context, key string, q LocationQuery, resolved Location, now time.Time) (*Weather, error) {
	query := url.Values{"key": {key}, "q": {q.providerPath()}, "days": {"7"}, "aqi": {"no"}, "alerts": {"no"}}
	var data waResponse
	if err := getJSON(ctx, weatherAPIProvider, WeatherAPIURL, query, true, &data); err != nil {
		return nil, err
	}
	return parseWeatherAPI(&data, resolved, now)
}

func parseWeatherAPI(data *waResponse, resolved Location, now time.Time) (*Weather, error) {
	current, err := waHourly(data.Current)
	if err != nil {
		return nil, err
	}
	var hourly []Hourly
	for _, day := range data.Forecast.Forecastday {
		for _, h := range day.Hour {
			if len(hourly) >= 24 {
				break
			}
			at, err := parseLocal(weatherAPIProvider, "2006-01-02 15:04", h.Time)
			if err != nil {
				return nil, err
			}
			if at.Before(now) {
				continue
			}
			hour, err := waHourly(h)
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
	days := data.Forecast.Forecastday[:min(len(data.Forecast.Forecastday), 7)]
	daily := make([]Daily, 0, len(days))
	for _, day := range days {
		d, err := waDaily(day)
		if err != nil {
			return nil, err
		}
		daily = append(daily, d)
	}
	return buildWeather(current.current(), hourly, daily, resolved, now), nil
}

func waHourly(h waHour) (Hourly, error) {
	p := weatherAPIProvider
	out := Hourly{
		Condition:     waConditionFor(h.Condition.Code),
		Humidity:      percentage(h.Humidity),
		WindDirection: windDirection(h.WindDegree),
		RainChance:    percentage(h.ChanceOfRain),
		UVIndex:       uvIndex(h.UV),
		CloudCover:    percentage(h.Cloud),
		IsDay:         h.IsDay == 1,
	}
	var errs fieldErrors
	out.Temperature = errs.temp(p, h.TempC)
	out.FeelsLike = errs.temp(p, h.FeelslikeC)
	out.Dewpoint = errs.temp(p, h.DewpointC)
	out.WindSpeed = errs.speed(p, h.WindKph)
	out.WindGust = errs.speed(p, h.GustKph)
	out.Pressure = errs.pressure(p, h.PressureMb)
	out.Visibility = errs.km(p, h.VisKm)
	out.Precipitation = errs.precip(p, h.PrecipMm)
	return out, errs.err
}

func waDaily(fd waForecastDay) (Daily, error) {
	p := weatherAPIProvider
	d := fd.Day
	out := Daily{
		Condition:   waConditionFor(d.Condition.Code),
		HumidityAvg: percentage(d.Avghumidity),
		RainChance:  percentage(d.DailyChanceOfRain),
		UVIndexMax:  uvIndex(d.UV),
	}
	var errs fieldErrors
	out.TempHigh = errs.temp(p, d.MaxtempC)
	out.TempLow = errs.temp(p, d.MintempC)
	out.TempAvg = errs.temp(p, d.AvgtempC)
	out.WindSpeedMax = errs.speed(p, d.MaxwindKph)
	out.PrecipitationSum = errs.precip(p, d.TotalprecipMm)
	if errs.err != nil {
		return Daily{}, errs.err
	}
	var err error
	if out.Date, err = parseLocal(p, "2006-01-02", fd.Date); err != nil {
		return Daily{}, err
	}
	if out.Sunrise, err = wa12h(fd.Astro.Sunrise); err != nil {
		return Daily{}, err
	}
	if out.Sunset, err = wa12h(fd.Astro.Sunset); err != nil {
		return Daily{}, err
	}
	return out, nil
}

// wa12h parses "06:12 AM" (either case).
func wa12h(s string) (TimeOfDay, error) {
	t, err := time.Parse("03:04 PM", strings.ToUpper(strings.TrimSpace(s)))
	if err != nil {
		return TimeOfDay{}, parseError(weatherAPIProvider, err.Error())
	}
	return TimeOfDay{Hour: t.Hour(), Minute: t.Minute()}, nil
}

// waConditionFor is condition_from_code over WeatherAPI's codes.
func waConditionFor(code int) Condition {
	switch code {
	case 1000:
		return CondClear
	case 1003:
		return CondPartlyCloudy
	case 1006:
		return CondCloudy
	case 1009:
		return CondOvercast
	case 1030:
		return CondMist
	case 1135, 1147:
		return CondFog
	case 1150, 1153, 1168, 1171:
		return CondDrizzle
	case 1063, 1180, 1183, 1240:
		return CondLightRain
	case 1186, 1189, 1243:
		return CondRain
	case 1192, 1195, 1246:
		return CondHeavyRain
	case 1066, 1210, 1213, 1255:
		return CondLightSnow
	case 1216, 1219, 1258:
		return CondSnow
	case 1114, 1117, 1222, 1225:
		return CondHeavySnow
	case 1069, 1072, 1198, 1201, 1204, 1207, 1249, 1252:
		return CondSleet
	case 1237, 1261, 1264:
		return CondHail
	case 1087, 1273, 1276, 1279, 1282:
		return CondThunderstorm
	}
	return CondUnknown
}
