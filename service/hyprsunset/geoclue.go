package hyprsunset

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// GeoClue2 names (geoclue.rs).
const (
	geoclueService     = "org.freedesktop.GeoClue2"
	geoclueManagerPath = dbus.ObjectPath("/org/freedesktop/GeoClue2/Manager")
	geoclueManager     = "org.freedesktop.GeoClue2.Manager"
	geoclueClient      = "org.freedesktop.GeoClue2.Client"
	geoclueLocation    = "org.freedesktop.GeoClue2.Location"
	// accuracyCity is GeoClue's "city" level: enough for a sunrise
	// schedule, and the least invasive level that yields coordinates.
	accuracyCity = uint32(4)
	// fixTimeout bounds the wait for the first location fix.
	fixTimeout = 15 * time.Second
)

// Location is a resolved position in decimal degrees.
type Location struct {
	Latitude  float64
	Longitude float64
}

// QueryLocation resolves the position once over GeoClue2 on conn (the
// system bus in the shell): get a client, set DesktopId and the city
// accuracy, start it, and take the first LocationUpdated. Any failure
// (no daemon, no agent, denied, timeout) is an error the caller treats
// as "use the configured coordinates".
func QueryLocation(ctx context.Context, conn *dbus.Conn) (Location, error) {
	return queryLocation(ctx, conn, fixTimeout)
}

func queryLocation(ctx context.Context, conn *dbus.Conn, timeout time.Duration) (Location, error) {
	var clientPath dbus.ObjectPath
	if err := conn.Object(geoclueService, geoclueManagerPath).CallWithContext(ctx, geoclueManager+".GetClient", 0).Store(&clientPath); err != nil {
		return Location{}, fmt.Errorf("geoclue: get client: %w", err)
	}
	client := conn.Object(geoclueService, clientPath)
	set := func(prop string, value any) error {
		return client.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Set", 0, geoclueClient, prop, dbus.MakeVariant(value)).Err
	}
	// DesktopId is mandatory before Start; without a requested accuracy
	// the daemon yields nothing.
	if err := set("DesktopId", "wayle"); err != nil {
		return Location{}, fmt.Errorf("geoclue: desktop id: %w", err)
	}
	if err := set("RequestedAccuracyLevel", accuracyCity); err != nil {
		return Location{}, fmt.Errorf("geoclue: accuracy: %w", err)
	}
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(clientPath), dbus.WithMatchInterface(geoclueClient), dbus.WithMatchMember("LocationUpdated")}
	if err := conn.AddMatchSignal(match...); err != nil {
		return Location{}, fmt.Errorf("geoclue: match: %w", err)
	}
	defer func() { _ = conn.RemoveMatchSignal(match...) }()
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	stop := func() { _ = client.CallWithContext(context.Background(), geoclueClient+".Stop", 0).Err }
	if err := client.CallWithContext(ctx, geoclueClient+".Start", 0).Err; err != nil {
		return Location{}, fmt.Errorf("geoclue: start: %w", err)
	}
	defer stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return Location{}, ctx.Err()
		case <-deadline.C:
			return Location{}, errors.New("geoclue: location fix timed out")
		case sig := <-signals:
			if sig.Path != clientPath || sig.Name != geoclueClient+".LocationUpdated" || len(sig.Body) != 2 {
				continue
			}
			locPath, ok := sig.Body[1].(dbus.ObjectPath)
			if !ok {
				continue
			}
			return readLocation(ctx, conn.Object(geoclueService, locPath))
		}
	}
}

// readLocation reads the Latitude and Longitude of a Location object.
func readLocation(ctx context.Context, obj dbus.BusObject) (Location, error) {
	var loc Location
	for prop, dest := range map[string]*float64{"Latitude": &loc.Latitude, "Longitude": &loc.Longitude} {
		var v dbus.Variant
		if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, geoclueLocation, prop).Store(&v); err != nil {
			return Location{}, fmt.Errorf("geoclue: %s: %w", prop, err)
		}
		f, ok := v.Value().(float64)
		if !ok {
			return Location{}, fmt.Errorf("geoclue: %s is %T, want a double", prop, v.Value())
		}
		*dest = f
	}
	return loc, nil
}
