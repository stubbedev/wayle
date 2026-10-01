// Package weather is wayle-weather: forward geocoding, the Open-Meteo,
// Visual Crossing, and WeatherAPI.com providers behind one model, and
// the polling service the bar module and dropdown read.
package weather

import (
	"fmt"
	"math"
	"time"
)

// Weather is one fetch: the current conditions, the next 24 hours, the
// next 7 days, the resolved location, and today's sunrise and sunset.
type Weather struct {
	Current   Current
	Hourly    []Hourly
	Daily     []Daily
	Location  Location
	Astronomy Astronomy
	UpdatedAt time.Time
}

// Current is the conditions now.
type Current struct {
	Temperature   Temperature
	FeelsLike     Temperature
	Condition     Condition
	Humidity      Percentage
	WindSpeed     Speed
	WindDirection WindDirection
	WindGust      Speed
	UVIndex       UVIndex
	CloudCover    Percentage
	Pressure      Pressure
	Visibility    Distance
	Dewpoint      Temperature
	Precipitation Precipitation
	IsDay         bool
}

// Hourly is one forecast hour; Time is the location's local wall time.
type Hourly struct {
	Time          time.Time
	Temperature   Temperature
	FeelsLike     Temperature
	Condition     Condition
	Humidity      Percentage
	WindSpeed     Speed
	WindDirection WindDirection
	WindGust      Speed
	RainChance    Percentage
	UVIndex       UVIndex
	CloudCover    Percentage
	Pressure      Pressure
	Visibility    Distance
	Dewpoint      Temperature
	Precipitation Precipitation
	IsDay         bool
}

// Daily is one forecast day; Date is midnight of that local date.
type Daily struct {
	Date             time.Time
	Condition        Condition
	TempHigh         Temperature
	TempLow          Temperature
	TempAvg          Temperature
	HumidityAvg      Percentage
	WindSpeedMax     Speed
	RainChance       Percentage
	UVIndexMax       UVIndex
	PrecipitationSum Precipitation
	Sunrise          TimeOfDay
	Sunset           TimeOfDay
}

// Location is where the forecast is for. Coordinates passed directly
// resolve with an empty city and country.
type Location struct {
	City    string
	Region  string
	Country string
	Lat     float64
	Lon     float64
}

// Astronomy is today's sun times.
type Astronomy struct {
	Sunrise TimeOfDay
	Sunset  TimeOfDay
}

// TimeOfDay is a local wall-clock time (chrono's NaiveTime at minute
// precision).
type TimeOfDay struct{ Hour, Minute int }

// String is HH:MM.
func (t TimeOfDay) String() string { return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute) }

// minutes orders times of day.
func (t TimeOfDay) minutes() int { return t.Hour*60 + t.Minute }

// LocationQuery is what to forecast: coordinates, or a city name with
// an optional ISO country code.
type LocationQuery struct {
	Coordinates bool
	Lat, Lon    float64
	City        string
	Country     string
}

// Coords is a coordinate query.
func Coords(lat, lon float64) LocationQuery {
	return LocationQuery{Coordinates: true, Lat: lat, Lon: lon}
}

// City is a city-name query.
func City(name string) LocationQuery { return LocationQuery{City: name} }

// providerPath is the location segment Visual Crossing and WeatherAPI
// take: "lat,lon", "name,country", or the name.
func (q LocationQuery) providerPath() string {
	switch {
	case q.Coordinates:
		return fmt.Sprintf("%v,%v", q.Lat, q.Lon)
	case q.Country != "":
		return q.City + "," + q.Country
	}
	return q.City
}

// Condition is model.rs WeatherCondition.
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
	case 56, 57, 66, 67:
		return CondSleet
	case 61:
		return CondLightRain
	case 63:
		return CondRain
	case 65:
		return CondHeavyRain
	case 71:
		return CondLightSnow
	case 73, 77, 85, 86:
		return CondSnow
	case 75:
		return CondHeavySnow
	case 80, 81, 82:
		return CondRain
	case 95, 96, 99:
		return CondThunderstorm
	}
	return CondUnknown
}

// ProviderKind is model.rs WeatherProviderKind.
type ProviderKind int

// Providers.
const (
	OpenMeteo ProviderKind = iota
	VisualCrossing
	WeatherAPI
)

// Temperature is degrees Celsius.
type Temperature float32

// Celsius is the stored value.
func (t Temperature) Celsius() float32 { return float32(t) }

// Fahrenheit converts.
func (t Temperature) Fahrenheit() float32 { return float32(t)*9/5 + 32 }

// Speed is km/h.
type Speed float32

// Kmh is the stored value.
func (s Speed) Kmh() float32 { return float32(s) }

// Mph converts.
func (s Speed) Mph() float32 { return float32(s) * 0.621371 }

// Distance is kilometers.
type Distance float32

// Km is the stored value.
func (d Distance) Km() float32 { return float32(d) }

// Miles converts.
func (d Distance) Miles() float32 { return float32(d) * 0.621371 }

// Pressure is hectopascals.
type Pressure float32

// Hpa is the stored value.
func (p Pressure) Hpa() float32 { return float32(p) }

// InHg converts.
func (p Pressure) InHg() float32 { return float32(p) * 0.02953 }

// Precipitation is millimeters.
type Precipitation float32

// Mm is the stored value.
func (p Precipitation) Mm() float32 { return float32(p) }

// Inches converts.
func (p Precipitation) Inches() float32 { return float32(p) * 0.0393701 }

// Percentage is 0..100.
type Percentage uint8

// WindDirection is degrees 0..359.
type WindDirection uint16

// Cardinal is the eight-point compass name.
func (d WindDirection) Cardinal() string {
	switch {
	case d <= 22 || d >= 338:
		return "N"
	case d <= 67:
		return "NE"
	case d <= 112:
		return "E"
	case d <= 157:
		return "SE"
	case d <= 202:
		return "S"
	case d <= 247:
		return "SW"
	case d <= 292:
		return "W"
	}
	return "NW"
}

// UVIndex is 0..15.
type UVIndex uint8

// RiskLevel is the WHO band name.
func (u UVIndex) RiskLevel() string {
	switch {
	case u <= 2:
		return "Low"
	case u <= 5:
		return "Moderate"
	case u <= 7:
		return "High"
	case u <= 10:
		return "Very High"
	}
	return "Extreme"
}

// The measurement constructors are the providers' parse helpers: the
// saturating kinds clamp, the rest reject a non-finite (or negative)
// reading as a parse error, as types.rs's new() does.

func percentage(v float64) Percentage { return Percentage(math.Max(0, math.Min(100, v))) }

func windDirection(v float64) WindDirection {
	return WindDirection(uint16(math.Max(0, v)) % 360)
}

func uvIndex(v float64) UVIndex { return UVIndex(math.Max(0, math.Min(15, v))) }

func temperature(provider string, celsius float64) (Temperature, error) {
	if math.IsNaN(celsius) || math.IsInf(celsius, 0) {
		return 0, parseError(provider, "invalid temperature")
	}
	return Temperature(celsius), nil
}

// nonNegative reads a quantity that cannot be negative: below zero
// clamps to zero (the providers' .max(0.0)), non-finite is an error.
func nonNegative(provider, what string, v float64) (float32, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, parseError(provider, "invalid "+what)
	}
	return float32(math.Max(0, v)), nil
}

func speed(provider string, kmh float64) (Speed, error) {
	v, err := nonNegative(provider, "speed", kmh)
	return Speed(v), err
}

func pressure(provider string, hpa float64) (Pressure, error) {
	v, err := nonNegative(provider, "pressure", hpa)
	return Pressure(v), err
}

func distanceKm(provider string, km float64) (Distance, error) {
	v, err := nonNegative(provider, "visibility", km)
	return Distance(v), err
}

func precipitation(provider string, mm float64) (Precipitation, error) {
	v, err := nonNegative(provider, "precipitation", mm)
	return Precipitation(v), err
}

// buildWeather is provider/mod.rs build_weather: today's sun times come
// from the first forecast day, 06:00/18:00 without one.
func buildWeather(current Current, hourly []Hourly, daily []Daily, location Location, now time.Time) *Weather {
	astro := Astronomy{Sunrise: TimeOfDay{Hour: 6}, Sunset: TimeOfDay{Hour: 18}}
	if len(daily) > 0 {
		astro = Astronomy{Sunrise: daily[0].Sunrise, Sunset: daily[0].Sunset}
	}
	return &Weather{Current: current, Hourly: hourly, Daily: daily, Location: location, Astronomy: astro, UpdatedAt: now}
}
