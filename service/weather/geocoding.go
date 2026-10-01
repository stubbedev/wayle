package weather

import (
	"context"
	"net/url"
)

const geocodingProvider = "geocoding"

// resolve is geocoding.rs resolve: coordinates pass through with no
// place name; a city is forward-geocoded through Open-Meteo, the first
// hit winning.
func resolve(ctx context.Context, q LocationQuery) (Location, error) {
	if q.Coordinates {
		return Location{Lat: q.Lat, Lon: q.Lon}, nil
	}
	query := url.Values{"name": {q.City}, "count": {"1"}}
	if q.Country != "" {
		query.Set("countryCode", q.Country)
	}
	var reply struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
		} `json:"results"`
	}
	if err := getJSON(ctx, geocodingProvider, GeocodingURL, query, false, &reply); err != nil {
		return Location{}, err
	}
	if len(reply.Results) == 0 {
		return Location{}, locationNotFound(q.City)
	}
	r := reply.Results[0]
	return Location{City: r.Name, Region: r.Admin1, Country: r.Country, Lat: r.Latitude, Lon: r.Longitude}, nil
}
