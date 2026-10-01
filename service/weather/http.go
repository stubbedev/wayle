package weather

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The endpoints; tests repoint them at a local server.
var (
	GeocodingURL      = "https://geocoding-api.open-meteo.com/v1/search"
	OpenMeteoURL      = "https://api.open-meteo.com/v1/forecast"
	VisualCrossingURL = "https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline"
	WeatherAPIURL     = "https://api.weatherapi.com/v1/forecast.json"
)

// httpClient is shared by every request; the timeout stands in for
// reqwest's connect/read limits.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// getJSON GETs endpoint?query and decodes the reply into into. auth
// marks providers whose 401/403 means a bad key (Visual Crossing and
// WeatherAPI): those report ErrAPIKeyMissing. 429 is a rate limit, any
// other non-2xx a status error.
func getJSON(ctx context.Context, provider, endpoint string, query url.Values, auth bool, into any) error {
	u := endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return httpError(provider, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return httpError(provider, err)
	}
	defer resp.Body.Close()
	switch {
	case auth && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden):
		return apiKeyMissing(provider)
	case resp.StatusCode == http.StatusTooManyRequests:
		return rateLimited(provider)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return statusError(provider, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpError(provider, err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return parseError(provider, err.Error())
	}
	return nil
}
