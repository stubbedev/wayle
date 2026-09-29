// Package mpris reads media player state over MPRIS2 (D-Bus), the Go
// counterpart of crates/wayle-media's zbus watcher: the first active
// player's metadata and playback status, with PropertiesChanged ticks.
package mpris

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	busNamePrefix = "org.mpris.MediaPlayer2."
	playerPath    = "/org/mpris/MediaPlayer2"
	playerIface   = "org.mpris.MediaPlayer2.Player"
	properties    = "org.freedesktop.DBus.Properties"
)

// PlaybackState is the player's transport state.
type PlaybackState uint8

// Playback states.
const (
	StateStopped PlaybackState = iota
	StatePlaying
	StatePaused
)

// StateFromString maps MPRIS's PlaybackStatus.
func StateFromString(status string) PlaybackState {
	switch strings.TrimSpace(status) {
	case "Playing":
		return StatePlaying
	case "Paused":
		return StatePaused
	}
	return StateStopped
}

// Track is one player's snapshot.
type Track struct {
	Player string // the bus name suffix after org.mpris.MediaPlayer2.
	Title  string
	Artist string
	Album  string
	State  PlaybackState
}

// Source is the module's seam.
type Source interface {
	// Active returns the first playing player's track, or the first
	// known player's when none plays (the Rust watcher's pick order).
	Active(ctx context.Context) (Track, error)
	// Subscribe ticks when a player's state or metadata may have
	// changed. The channel closes when ctx completes; stop releases.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}

// Session is the real source on the session bus.
type Session struct {
	mu   sync.Mutex
	conn *dbus.Conn
}

// NewSession connects to the session bus.
func NewSession() (*Session, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("mpris: session bus: %w", err)
	}
	return &Session{conn: conn}, nil
}

// Close drops the bus connection.
func (s *Session) Close() error { return s.conn.Close() }

// playerNames lists the connected MPRIS players from the bus names.
func (s *Session) playerNames() ([]string, error) {
	var names []string
	err := s.conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		return nil, fmt.Errorf("mpris: list names: %w", err)
	}
	var players []string
	for _, name := range names {
		if suffix, ok := strings.CutPrefix(name, busNamePrefix); ok {
			players = append(players, suffix)
		}
	}
	return players, nil
}

// Active picks the playing player, else the first player.
func (s *Session) Active(ctx context.Context) (Track, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	players, err := s.playerNames()
	if err != nil {
		return Track{}, err
	}
	if len(players) == 0 {
		return Track{}, nil
	}
	var fallback Track
	found := false
	for _, player := range players {
		track, err := s.readPlayer(ctx, player)
		if err != nil {
			continue
		}
		if !found {
			fallback = track
			found = true
		}
		if track.State == StatePlaying {
			return track, nil
		}
	}
	if !found {
		return Track{}, fmt.Errorf("mpris: no readable player of %d", len(players))
	}
	return fallback, nil
}

// readPlayer pulls one player's status and metadata.
func (s *Session) readPlayer(ctx context.Context, player string) (Track, error) {
	obj := s.conn.Object(busNamePrefix+player, dbus.ObjectPath(playerPath))
	track := Track{Player: player}

	var status string
	if err := obj.CallWithContext(ctx, properties+".Get", 0, playerIface, "PlaybackStatus").Store(&status); err != nil {
		return track, err
	}
	track.State = StateFromString(status)

	var metadata map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, properties+".Get", 0, playerIface, "Metadata").Store(&metadata); err != nil {
		return track, err
	}
	track.Title = variantString(metadata["xesam:title"])
	artist := variantStringArray(metadata["xesam:artist"])
	track.Artist = strings.Join(artist, ", ")
	track.Album = variantString(metadata["xesam:album"])
	return track, nil
}

func variantString(v dbus.Variant) string {
	s, ok := v.Value().(string)
	if !ok {
		return ""
	}
	return s
}

func variantStringArray(v dbus.Variant) []string {
	switch value := v.Value().(type) {
	case []string:
		return value
	case string:
		return []string{value}
	}
	return nil
}

// Subscribe matches PropertiesChanged from any MPRIS player.
func (s *Session) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	if err := s.conn.AddMatchSignal(
		dbus.WithMatchInterface(properties),
		dbus.WithMatchArg(0, playerIface),
	); err != nil {
		return nil, nil, fmt.Errorf("mpris: match signal: %w", err)
	}
	ticks := make(chan struct{}, 1)
	signals := make(chan *dbus.Signal, 16)
	s.conn.Signal(signals)
	go func() {
		defer close(ticks)
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				if name, ok := sig.Body[1].(string); ok && name != playerIface {
					continue
				}
				tick(ticks)
			}
		}
	}()
	stop := func() {
		_ = s.conn.RemoveMatchSignal(
			dbus.WithMatchInterface(properties),
			dbus.WithMatchArg(0, playerIface),
		)
		s.conn.RemoveSignal(signals)
	}
	return ticks, stop, nil
}

func tick(ticks chan struct{}) {
	select {
	case ticks <- struct{}{}:
	default:
	}
}
