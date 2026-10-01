package weather

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/wayle/internal/feed"
)

// Settings is everything a poll depends on (the builder's fields and
// the service setters). Keys are already resolved secrets; empty is
// unset.
type Settings struct {
	Interval          time.Duration
	Provider          ProviderKind
	Location          LocationQuery
	VisualCrossingKey string
	WeatherAPIKey     string
}

// ParseLocation is the shell's parse_location: "lat,lon" when both
// halves are numbers, otherwise a city name.
func ParseLocation(s string) LocationQuery {
	if lat, lon, ok := strings.Cut(s, ","); ok {
		la, err1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
		lo, err2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
		if err1 == nil && err2 == nil {
			return Coords(la, lo)
		}
	}
	return City(s)
}

// StatusKind is the fetch lifecycle (service.rs WeatherStatus).
type StatusKind int

// Statuses.
const (
	Loading StatusKind = iota
	Loaded
	Failed
)

// Status is the lifecycle with the failure of Failed: its category,
// the provider missing a key, and the location that did not resolve.
type Status struct {
	Kind     StatusKind
	Error    ErrorKind
	Provider string
	Query    string
}

// retry policy (polling.rs).
const (
	maxRetries        = 3
	initialRetryDelay = 5 * time.Second
	rateLimitDelay    = 60 * time.Second
)

// FetchFunc fetches one forecast: the providers, or a stand-in.
type FetchFunc func(ctx context.Context, s Settings, now time.Time) (*Weather, error)

// Service polls the configured provider and caches the last successful
// fetch. After the first poll it only fetches while something is
// subscribed (polling.rs skips ticks without subscribers).
type Service struct {
	mu       sync.Mutex
	settings Settings
	weather  *Weather
	status   Status
	cancel   context.CancelFunc
	life     context.Context
	stop     context.CancelFunc
	changes  *feed.Tick
	fetch    FetchFunc
	// retryDelay is the backoff; tests shorten it.
	retryDelay func(err error, attempt int) time.Duration
}

// New starts a service polling with s; Close stops it.
func New(s Settings) *Service {
	return newService(s, fetchWeather, retryDelay)
}

// NewWith polls through fetch instead of the providers (a fixture, a
// demo); retries and subscriber gating behave as in New.
func NewWith(s Settings, fetch FetchFunc) *Service {
	return newService(s, fetch, retryDelay)
}

func newService(s Settings, fetch FetchFunc, delay func(error, int) time.Duration) *Service {
	life, stop := context.WithCancel(context.Background())
	svc := &Service{life: life, stop: stop, changes: feed.NewTick(), fetch: fetch, retryDelay: delay}
	svc.mu.Lock()
	svc.restartLocked(s)
	svc.mu.Unlock()
	return svc
}

// Configure applies new settings; unchanged settings keep the running
// poll (a reload that touched other keys does not refetch).
func (s *Service) Configure(next Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if next == s.settings {
		return
	}
	s.restartLocked(next)
}

// restartLocked is restart_polling: status back to loading, the old
// poll canceled, a new one started.
func (s *Service) restartLocked(next Settings) {
	s.settings = next
	s.status = Status{Kind: Loading}
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(s.life)
	s.cancel = cancel
	go s.poll(ctx, next)
	feed.Notify(s.changes)
}

// Refresh restarts polling with the current settings: the dropdown's
// refresh and retry buttons.
func (s *Service) Refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restartLocked(s.settings)
}

// Weather is the last successful fetch; nil before the first.
func (s *Service) Weather() *Weather {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.weather
}

// Status is the fetch lifecycle.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Subscribe ticks whenever Weather or Status changed.
func (s *Service) Subscribe() (<-chan struct{}, func()) { return s.changes.Subscribe() }

// Close stops polling.
func (s *Service) Close() { s.stop() }

func (s *Service) poll(ctx context.Context, settings Settings) {
	interval := settings.Interval
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	first := true
	for {
		if first || s.changes.Len() > 0 {
			first = false
			weather, err := s.fetchWithRetry(ctx, settings)
			if ctx.Err() != nil {
				return
			}
			s.publish(ctx, weather, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// publish stores one poll's outcome unless the poll was superseded.
func (s *Service) publish(ctx context.Context, weather *Weather, err error) {
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.status = failedStatus(err)
	} else {
		s.weather = weather
		s.status = Status{Kind: Loaded}
	}
	s.mu.Unlock()
	feed.Notify(s.changes)
}

// failedStatus is WeatherStatus::Error with the kind's details.
func failedStatus(err error) Status {
	st := Status{Kind: Failed, Error: ErrOther}
	if e, ok := errors.AsType[*Error](err); ok {
		st.Error, st.Provider, st.Query = e.Kind, e.Provider, e.Query
	}
	return st
}

// fetchWithRetry is fetch_with_retry: retryable failures back off and
// retry up to three attempts.
func (s *Service) fetchWithRetry(ctx context.Context, settings Settings) (*Weather, error) {
	for attempt := 1; ; attempt++ {
		weather, err := s.fetch(ctx, settings, time.Now())
		if err == nil {
			return weather, nil
		}
		var e *Error
		if attempt >= maxRetries || !errors.As(err, &e) || !e.Retryable() {
			log.Printf("weather: cannot fetch weather data: %v", err)
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(s.retryDelay(err, attempt)):
		}
	}
}

// retryDelay doubles from 5s; a rate limit waits a minute.
func retryDelay(err error, attempt int) time.Duration {
	if kindOf(err) == ErrRateLimited {
		return rateLimitDelay
	}
	return initialRetryDelay << (attempt - 1)
}

// fetchWeather resolves the location and fetches from the configured
// provider; a keyed provider without its key is ErrAPIKeyMissing.
func fetchWeather(ctx context.Context, s Settings, now time.Time) (*Weather, error) {
	resolved, err := resolve(ctx, s.Location)
	if err != nil {
		log.Printf("weather: cannot resolve location: %v", err)
		return nil, err
	}
	switch s.Provider {
	case VisualCrossing:
		if s.VisualCrossingKey == "" {
			return nil, apiKeyMissing(visualCrossingProvider)
		}
		return fetchVisualCrossing(ctx, s.VisualCrossingKey, s.Location, resolved, now)
	case WeatherAPI:
		if s.WeatherAPIKey == "" {
			return nil, apiKeyMissing(weatherAPIProvider)
		}
		return fetchWeatherAPI(ctx, s.WeatherAPIKey, s.Location, resolved, now)
	}
	return fetchOpenMeteo(ctx, resolved, now)
}
